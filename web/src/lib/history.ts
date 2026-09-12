import type { ExchangeSummary } from '$lib/api/types';

export function mergeNewest(current: ExchangeSummary[], incoming: ExchangeSummary[]): ExchangeSummary[] {
  const byID = new Map(current.map((item) => [item.id, item]));
  for (const item of incoming) {
    const previous = byID.get(item.id);
    if (!previous || item.revision >= previous.revision) byID.set(item.id, item);
  }
  return [...byID.values()].sort(compareNewest);
}

export function appendOlder(current: ExchangeSummary[], page: ExchangeSummary[]): ExchangeSummary[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...page.filter((item) => !seen.has(item.id))];
}

export function visibleWindow(scrollTop: number, viewportHeight: number, count: number, rowHeight = 64): { start: number; end: number; offset: number; total: number } {
  const overscan = 4;
  const start = Math.min(count, Math.max(0, Math.floor(scrollTop / rowHeight) - overscan));
  const end = Math.min(count, Math.ceil((scrollTop + viewportHeight) / rowHeight) + overscan);
  return { start, end, offset: start * rowHeight, total: count * rowHeight };
}

function compareNewest(left: ExchangeSummary, right: ExchangeSummary): number {
  const leftTime = Date.parse(left.completed_at ?? left.started_at);
  const rightTime = Date.parse(right.completed_at ?? right.started_at);
  if (leftTime !== rightTime) return rightTime - leftTime;
  return right.id.localeCompare(left.id);
}

export function historyTotalCount(
  runtime?: { ram_exchanges?: number; storage?: { enabled: boolean; exchange_count: number } },
  loadedCount = 0
): number {
  if (runtime?.storage?.enabled) {
    return Math.max(runtime.storage.exchange_count, loadedCount);
  }
  return Math.max(runtime?.ram_exchanges ?? 0, loadedCount);
}

export function decrementHistoryCount<T extends { ram_exchanges?: number; storage?: { enabled: boolean; exchange_count: number } }>(
  runtime: T | undefined
): T | undefined {
  if (!runtime) return undefined;
  return {
    ...runtime,
    ram_exchanges: Math.max(0, (runtime.ram_exchanges ?? 0) - 1),
    storage: runtime.storage?.enabled
      ? { ...runtime.storage, exchange_count: Math.max(0, runtime.storage.exchange_count - 1) }
      : runtime.storage
  };
}

export function clearHistoryCount<T extends { ram_exchanges?: number; storage?: { enabled: boolean; exchange_count: number } }>(
  runtime: T | undefined
): T | undefined {
  if (!runtime) return undefined;
  return {
    ...runtime,
    ram_exchanges: 0,
    storage: runtime.storage?.enabled
      ? { ...runtime.storage, exchange_count: 0 }
      : runtime.storage
  };
}

export function formatHistoryLabel(total: number): string {
  return `History (${total})`;
}

export function defaultHistorySelection(
  selected: string | undefined,
  items: ExchangeSummary[]
): string | undefined {
  if (selected !== undefined && selected !== '') {
    return selected;
  }
  return items.length > 0 ? items[0].id : undefined;
}

export function nextHistorySelection(
  currentSelected: string | undefined,
  deletedID: string,
  remainingItems: ExchangeSummary[]
): string | undefined {
  if (currentSelected !== deletedID) {
    return currentSelected;
  }
  return remainingItems.length > 0 ? remainingItems[0].id : undefined;
}
