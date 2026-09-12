import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { appendOlder, clearHistoryCount, decrementHistoryCount, defaultHistorySelection, formatHistoryLabel, historyTotalCount, mergeNewest, nextHistorySelection, visibleWindow } from './history';
import type { ExchangeSummary } from './api/types';

const summary = (id: string, revision = 1, second = 0): ExchangeSummary => ({
  id, revision, mode: 'capture', state: 'completed', config_revision: 1,
  started_at: `2026-09-07T10:00:${String(second).padStart(2, '0')}Z`, method: 'GET', path: '/',
  request_body_bytes: 0, response_body_bytes: 0, duration_ns: 1, preview_truncated: false
});
describe('history transformations', () => {
  it('deduplicates RAM, SSE, and SQLite rows by ID while retaining newest revisions', () => {
    const result = mergeNewest([summary('a', 1), summary('b', 1)], [summary('a', 2), summary('c', 1)]);
    expect(result.map((item) => item.id).sort()).toEqual(['a', 'b', 'c']);
    expect(result.find((item) => item.id === 'a')?.revision).toBe(2);
    expect(appendOlder(result, [summary('b'), summary('d')]).map((item) => item.id)).toHaveLength(4);
  });

  it('always creates a bounded valid virtual window', () => {
    fc.assert(fc.property(
      fc.nat({ max: 100_000 }), fc.nat({ max: 4_000 }), fc.nat({ max: 10_000 }),
      (scrollTop, height, count) => {
        const window = visibleWindow(scrollTop, height, count);
        expect(window.start).toBeGreaterThanOrEqual(0);
        expect(window.end).toBeGreaterThanOrEqual(window.start);
        expect(window.end).toBeLessThanOrEqual(count);
        expect(window.total).toBe(count * 64);
      }
    ), { seed: 9072026, numRuns: 500 });
  });

  it('formats history tab label with total count only without max capacity', () => {
    expect(formatHistoryLabel(0)).toBe('History (0)');
    expect(formatHistoryLabel(42)).toBe('History (42)');
    expect(formatHistoryLabel(100)).not.toContain('/');
  });

  it('resolves total history count across RAM, SQLite, and client-side fallback', () => {
    expect(historyTotalCount(undefined, 0)).toBe(0);
    expect(historyTotalCount(undefined, 5)).toBe(5);

    // RAM mode
    expect(historyTotalCount({ ram_exchanges: 12 }, 0)).toBe(12);
    expect(historyTotalCount({ ram_exchanges: 12, storage: { enabled: false, exchange_count: 0 } }, 12)).toBe(12);

    // SQLite mode
    expect(historyTotalCount({ ram_exchanges: 0, storage: { enabled: true, exchange_count: 250 } }, 50)).toBe(250);
  });

  it('preserves history label invariants under property generation', () => {
    fc.assert(fc.property(
      fc.nat({ max: 1_000_000 }),
      (total) => {
        const label = formatHistoryLabel(total);
        expect(label).toBe(`History (${total})`);
        expect(label.includes('/')).toBe(false);
      }
    ), { numRuns: 200 });
  });

  it('selects newest exchange when opening history with no selection', () => {
    const items = [summary('newest', 1, 10), summary('older', 1, 5)];
    expect(defaultHistorySelection(undefined, items)).toBe('newest');
    expect(defaultHistorySelection('', items)).toBe('newest');
    expect(defaultHistorySelection('existing', items)).toBe('existing');
    expect(defaultHistorySelection(undefined, [])).toBeUndefined();
  });

  it('preserves selection property when items change', () => {
    fc.assert(fc.property(
      fc.string({ minLength: 1 }),
      fc.array(fc.string({ minLength: 1 }).map((id) => summary(id)), { minLength: 1, maxLength: 20 }),
      (selectedId, items) => {
        expect(defaultHistorySelection(selectedId, items)).toBe(selectedId);
        expect(defaultHistorySelection(undefined, items)).toBe(items[0].id);
      }
    ), { numRuns: 100 });
  });

  it('updates selection when current selected item is deleted', () => {
    const remaining = [summary('second'), summary('third')];
    expect(nextHistorySelection('first', 'first', remaining)).toBe('second');
    expect(nextHistorySelection('other', 'first', remaining)).toBe('other');
    expect(nextHistorySelection('first', 'first', [])).toBeUndefined();
  });

  it('maintains nextHistorySelection invariant across arbitrary inputs', () => {
    fc.assert(fc.property(
      fc.string({ minLength: 1 }),
      fc.string({ minLength: 1 }),
      fc.array(fc.string({ minLength: 1 }).map((id) => summary(id)), { maxLength: 10 }),
      (currentSelected, deletedID, remaining) => {
        const next = nextHistorySelection(currentSelected, deletedID, remaining);
        if (currentSelected === deletedID) {
          expect(next).toBe(remaining.length > 0 ? remaining[0].id : undefined);
        } else {
          expect(next).toBe(currentSelected);
        }
      }
    ), { numRuns: 200 });
  });

  it('decrements and clears runtime history count correctly', () => {
    expect(decrementHistoryCount(undefined)).toBeUndefined();
    expect(clearHistoryCount(undefined)).toBeUndefined();

    // RAM mode
    const ramRuntime = { ram_exchanges: 5 };
    expect(decrementHistoryCount(ramRuntime)?.ram_exchanges).toBe(4);
    expect(decrementHistoryCount({ ram_exchanges: 0 })?.ram_exchanges).toBe(0);
    expect(clearHistoryCount(ramRuntime)?.ram_exchanges).toBe(0);

    // SQLite mode
    const sqlRuntime = { ram_exchanges: 5, storage: { enabled: true, exchange_count: 50 } };
    const decrementedSql = decrementHistoryCount(sqlRuntime);
    expect(decrementedSql?.ram_exchanges).toBe(4);
    expect(decrementedSql?.storage?.exchange_count).toBe(49);
    const clearedSql = clearHistoryCount(sqlRuntime);
    expect(clearedSql?.ram_exchanges).toBe(0);
    expect(clearedSql?.storage?.exchange_count).toBe(0);
  });

  it('preserves non-negative count invariants when decrementing or clearing', () => {
    fc.assert(fc.property(
      fc.nat({ max: 10_000 }),
      fc.boolean(),
      fc.nat({ max: 10_000 }),
      (ram, storageEnabled, storageCount) => {
        const rt = { ram_exchanges: ram, storage: { enabled: storageEnabled, exchange_count: storageCount } };
        const dec = decrementHistoryCount(rt);
        expect(dec?.ram_exchanges).toBe(Math.max(0, ram - 1));
        if (storageEnabled) {
          expect(dec?.storage?.exchange_count).toBe(Math.max(0, storageCount - 1));
        }
        const clr = clearHistoryCount(rt);
        expect(clr?.ram_exchanges).toBe(0);
        if (storageEnabled) {
          expect(clr?.storage?.exchange_count).toBe(0);
        }
      }
    ), { numRuns: 200 });
  });
});
