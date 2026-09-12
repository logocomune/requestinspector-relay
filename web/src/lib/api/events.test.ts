import { describe, expect, it } from 'vitest';
import fc from 'fast-check';
import { reduceStream, type StreamState } from './events';
import type { ExchangeSummary, LifecycleEnvelope } from './types';

const item = (id: string): ExchangeSummary => ({
  id, mode: 'capture', state: 'completed', revision: 1, config_revision: 1,
  started_at: '2026-09-07T10:00:00Z', method: 'GET', path: '/', request_body_bytes: 0,
  response_body_bytes: 0, duration_ns: 1, preview_truncated: false
});

const envelope = (event_id: string, type: LifecycleEnvelope['type'], exchange_id?: string): LifecycleEnvelope => ({
  schema_version: 1, event_id, timestamp: '2026-09-07T10:00:00Z', type, exchange_id, revision: 1
});

describe('SSE reducer', () => {
  const empty = (): StreamState => ({ connection: 'live', cursor: '', items: [], refreshRequired: false });

  it('deduplicates snapshot items without changing newest-first order', () => {
    const state = reduceStream(empty(), { kind: 'snapshot', value: { schema_version: 1, event_cursor: '4', items: [item('b'), item('a'), item('b')] } });
    expect(state.items.map(({ id }) => id)).toEqual(['b', 'a']);
  });

  it('ignores duplicate and stale events', () => {
    const state = { ...empty(), cursor: '4' };
    expect(reduceStream(state, { kind: 'event', value: envelope('4', 'config.changed') })).toBe(state);
    expect(reduceStream(state, { kind: 'event', value: envelope('3', 'config.changed') })).toBe(state);
  });

  it('orders cursors beyond JavaScript safe integers', () => {
    const state = { ...empty(), cursor: '90071992547409930' };
    const next = reduceStream(state, { kind: 'event', value: envelope('90071992547409931', 'config.changed') });
    expect(next.cursor).toBe('90071992547409931');
  });

  it('removes evictions and clears history', () => {
    let state = { ...empty(), items: [item('a'), item('b')] };
    state = reduceStream(state, { kind: 'event', value: envelope('1', 'exchange.evicted', 'a') });
    expect(state.items.map(({ id }) => id)).toEqual(['b']);
    state = reduceStream(state, { kind: 'event', value: envelope('2', 'history.cleared') });
    expect(state.items).toEqual([]);
  });

  it('removes explicitly deleted exchanges', () => {
    const state = { ...empty(), items: [item('a'), item('b')] };
    const next = reduceStream(state, { kind: 'event', value: envelope('1', 'exchange.deleted', 'a') });
    expect(next.items.map(({ id }) => id)).toEqual(['b']);
  });

  it('keeps snapshot IDs unique for arbitrary duplicate streams', () => {
    fc.assert(fc.property(fc.array(fc.string({ minLength: 1, maxLength: 12 }), { maxLength: 200 }), (ids) => {
      const state = reduceStream(empty(), { kind: 'snapshot', value: { schema_version: 1, event_cursor: '1', items: ids.map(item) } });
      expect(state.items.map(({ id }) => id)).toEqual([...new Set(ids)]);
    }), { seed: 9072026, numRuns: 500 });
  });

  it('never moves a numeric event cursor backwards', () => {
    fc.assert(fc.property(fc.array(fc.nat({ max: 1_000_000 }), { maxLength: 200 }), (cursors) => {
      let state = empty();
      for (const cursor of cursors) state = reduceStream(state, { kind: 'event', value: envelope(String(cursor), 'config.changed') });
      expect(BigInt(state.cursor || '0')).toBe(cursors.length ? BigInt(Math.max(...cursors)) : 0n);
    }), { seed: 9072026, numRuns: 500 });
  });
});
