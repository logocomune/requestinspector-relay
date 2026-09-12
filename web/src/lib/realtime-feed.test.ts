import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { mergeRealtime, realtimeExchangeLimit, summaryFromExchange } from './realtime-feed';
import type { ExchangeSummary } from './api/types';

const summary = (id: string, revision: number, timestamp: number): ExchangeSummary => ({
  id,
  revision,
  mode: 'capture',
  state: 'completed',
  config_revision: 1,
  started_at: new Date(timestamp).toISOString(),
  method: 'GET',
  path: `/${id}`,
  request_body_bytes: 0,
  response_body_bytes: 0,
  duration_ns: 1,
  preview_truncated: false
});

describe('Realtime feed', () => {
  it('builds summary from received exchange detail without history lookup', () => {
    const result = summaryFromExchange({
      id: 'live', transport: 'http', mode: 'capture', config_revision: 2, revision: 3, state: 'completed',
      started_at: '2026-09-07T10:00:00Z', completed_at: '2026-09-07T10:00:01Z', duration_ns: 1,
      request: {
        method: 'POST', scheme: 'http', protocol: 'HTTP/1.1', host: 'localhost', request_target: '/live', path: '/live', raw_query: '',
        remote_address: '127.0.0.1', headers: {}, content_type: 'text/plain', observed_body_bytes: 4, body_complete: true,
        header_bytes_estimate: 10, preview: 'body', preview_truncated: false
      },
      response: { status: 201, headers: {}, observed_body_bytes: 2, body_complete: true, header_bytes_estimate: 5, origin: 'upstream', preview_truncated: false }
    });
    expect(result).toMatchObject({ id: 'live', revision: 3, path: '/live', request_body_bytes: 4, response_body_bytes: 2, response_status: 201 });
  });

  it('builds UDP summary without HTTP fields', () => {
    const result = summaryFromExchange({
      id: 'udp-live', transport: 'udp', mode: 'capture', config_revision: 2, revision: 3, state: 'completed',
      started_at: '2026-09-08T12:00:00Z', completed_at: '2026-09-08T12:00:01Z', duration_ns: 1,
      datagram: {
        source_address: '127.0.0.1:5000', local_address: '127.0.0.1:9000', accepted_bytes: 4,
        payload_complete: true, preview_truncated: false,
        delivery: { result: 'not_sent', sent_bytes: 0, truncated: false }
      }
    });
    expect(result).toMatchObject({ transport: 'udp', source_address: '127.0.0.1:5000', local_address: '127.0.0.1:9000', datagram_bytes: 4 });
    expect('method' in result).toBe(false);
  });

  it('keeps newest 50 exchanges and discards oldest visible entries', () => {
    const incoming = Array.from({ length: 60 }, (_, index) => summary(String(index), 1, index));

    const result = mergeRealtime([], incoming);

    expect(result).toHaveLength(realtimeExchangeLimit);
    expect(result[0].summary.id).toBe('59');
    expect(result.at(-1)?.summary.id).toBe('10');
  });

  it('keeps loaded detail for unchanged revisions and resets changed revisions', () => {
    const first = summary('exchange', 1, 1);
    const current = [{ summary: first, detail: { exchange: { id: first.id, revision: 1 } } as never, error: '', unavailable: false }];

    expect(mergeRealtime(current, [first])[0].detail).toBe(current[0].detail);
    expect(mergeRealtime(current, [summary('exchange', 2, 1)])[0].detail).toBeUndefined();
  });

  it('appends incoming packets without removing earlier realtime packets', () => {
    const first = summary('first', 1, 1);
    const second = summary('second', 1, 2);
    const current = mergeRealtime([], [first]);
    expect(mergeRealtime(current, [second]).map((item) => item.summary.id)).toEqual(['second', 'first']);
  });

  it('always returns unique newest-first entries within the visible limit', () => {
    fc.assert(fc.property(
      fc.uniqueArray(fc.tuple(fc.uuid(), fc.integer({ min: 1, max: 1000 })), { selector: ([id]) => id, maxLength: 100 }),
      (values) => {
        const result = mergeRealtime([], values.map(([id, timestamp]) => summary(id, 1, timestamp)));
        expect(result.length).toBeLessThanOrEqual(realtimeExchangeLimit);
        expect(new Set(result.map((item) => item.summary.id)).size).toBe(result.length);
        for (let index = 1; index < result.length; index += 1) {
          expect(Date.parse(result[index - 1].summary.started_at)).toBeGreaterThanOrEqual(Date.parse(result[index].summary.started_at));
        }
      }
    ), { seed: 20260909, numRuns: 300 });
  });
});
