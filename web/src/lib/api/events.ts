import { isLifecycleEnvelope, isStreamSnapshot } from './contract';
import type { ExchangeSummary, LifecycleEnvelope, StreamResync, StreamSnapshot } from './types';

export type ConnectionState = 'connecting' | 'live' | 'reconnecting' | 'offline';

export interface StreamState {
  connection: ConnectionState;
  cursor: string;
  items: ExchangeSummary[];
  latest?: LifecycleEnvelope;
  refreshRequired: boolean;
}

export type StreamAction =
  | { kind: 'connection'; value: ConnectionState }
  | { kind: 'snapshot'; value: StreamSnapshot }
  | { kind: 'resync'; value: StreamResync }
  | { kind: 'event'; value: LifecycleEnvelope };

export const initialStreamState = (): StreamState => ({
  connection: navigator.onLine ? 'connecting' : 'offline', cursor: '', items: [], refreshRequired: false
});

export function reduceStream(state: StreamState, action: StreamAction): StreamState {
  if (action.kind === 'connection') return { ...state, connection: action.value };
  if (action.kind === 'snapshot' || action.kind === 'resync') {
    return { ...state, cursor: action.value.event_cursor, items: uniqueNewest(action.value.items), refreshRequired: false };
  }
  const event = action.value;
  if (event.event_id && compareCursor(event.event_id, state.cursor) <= 0) return state;
  if (event.type === 'history.cleared') return { ...state, cursor: event.event_id, items: [], latest: event, refreshRequired: false };
  if (event.type === 'exchange.evicted' || event.type === 'exchange.deleted') {
    return { ...state, cursor: event.event_id, items: state.items.filter((item) => item.id !== event.exchange_id), latest: event };
  }
  return { ...state, cursor: event.event_id, latest: event, refreshRequired: event.type !== 'config.changed' };
}

function uniqueNewest(items: ExchangeSummary[]): ExchangeSummary[] {
  const seen = new Set<string>();
  const unique: ExchangeSummary[] = [];
  for (const item of items) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    unique.push(item);
  }
  return unique;
}

function compareCursor(left: string, right: string): number {
  if (!right) return 1;
  if (/^\d+$/.test(left) && /^\d+$/.test(right)) {
    const leftNumber = BigInt(left);
    const rightNumber = BigInt(right);
    return leftNumber > rightNumber ? 1 : leftNumber < rightNumber ? -1 : 0;
  }
  return left.localeCompare(right);
}

function parseRecord(data: string): unknown {
  try { return JSON.parse(data); } catch { return undefined; }
}

export function openEventStream(dispatch: (action: StreamAction) => void): () => void {
  const source = new EventSource('/api/v1/events');
  source.onopen = () => dispatch({ kind: 'connection', value: 'live' });
  source.onerror = () => dispatch({ kind: 'connection', value: navigator.onLine ? 'reconnecting' : 'offline' });
  source.addEventListener('snapshot', (event) => {
    const value = parseRecord(event.data);
    if (isStreamSnapshot(value)) dispatch({ kind: 'snapshot', value });
  });
  source.addEventListener('resync', (event) => {
    const value = parseRecord(event.data);
    if (isStreamSnapshot(value) && 'reason' in value && value.reason === 'replay_gap') dispatch({ kind: 'resync', value: value as StreamResync });
  });
  for (const type of ['exchange.started', 'request.completed', 'response.completed', 'exchange.failed', 'exchange.evicted', 'exchange.deleted', 'history.cleared', 'config.changed']) {
    source.addEventListener(type, (event) => {
      const value = parseRecord(event.data);
      if (isLifecycleEnvelope(value)) dispatch({ kind: 'event', value });
    });
  }
  const offline = () => dispatch({ kind: 'connection', value: 'offline' });
  const online = () => dispatch({ kind: 'connection', value: 'reconnecting' });
  window.addEventListener('offline', offline);
  window.addEventListener('online', online);
  return () => {
    source.close();
    window.removeEventListener('offline', offline);
    window.removeEventListener('online', online);
  };
}
