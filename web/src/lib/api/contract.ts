import { lifecycleEventTypes, type ConfigurationView, type ExchangeDetail, type ExchangeList, type ExchangeSummary, type LifecycleEnvelope, type RuntimeStatus, type StreamSnapshot } from './types';

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const hasString = (value: Record<string, unknown>, key: string): boolean => typeof value[key] === 'string';
const hasNumber = (value: Record<string, unknown>, key: string): boolean => typeof value[key] === 'number';
const hasBoolean = (value: Record<string, unknown>, key: string): boolean => typeof value[key] === 'boolean';

export function isExchangeSummary(value: unknown): value is ExchangeSummary {
  if (!isRecord(value)) return false;
  const shared = hasString(value, 'id') && hasString(value, 'mode') && hasString(value, 'state') &&
    hasNumber(value, 'revision') && hasNumber(value, 'config_revision') && hasString(value, 'started_at') &&
    hasNumber(value, 'duration_ns');
  if (!shared) return false;
  if (value.transport === 'udp') {
    return hasString(value, 'source_address') && hasString(value, 'local_address') && hasNumber(value, 'datagram_bytes');
  }
  return (value.transport === undefined || value.transport === 'http') && hasString(value, 'method') &&
    hasString(value, 'path') && hasNumber(value, 'request_body_bytes') && hasNumber(value, 'response_body_bytes') &&
    typeof value.preview_truncated === 'boolean';
}

export function isLifecycleEnvelope(value: unknown): value is LifecycleEnvelope {
  if (!isRecord(value) || value.schema_version !== 1 || !hasString(value, 'event_id') ||
    !hasString(value, 'timestamp') || !hasNumber(value, 'revision') || !hasString(value, 'type')) return false;
  return lifecycleEventTypes.includes(value.type as LifecycleEnvelope['type']);
}

export function isStreamSnapshot(value: unknown): value is StreamSnapshot {
  return isRecord(value) && value.schema_version === 1 && hasString(value, 'event_cursor') &&
    Array.isArray(value.items) && value.items.every(isExchangeSummary);
}

export function isRuntimeStatus(value: unknown): value is RuntimeStatus {
  return isRecord(value) && hasString(value, 'build') && hasString(value, 'commit') && hasString(value, 'build_date') && hasString(value, 'mode') &&
    hasString(value, 'management_listener') && hasString(value, 'traffic_listener') &&
    hasNumber(value, 'uptime_seconds') && hasNumber(value, 'ram_exchanges') &&
    hasNumber(value, 'ram_capacity') && hasNumber(value, 'config_revision') &&
    hasNumber(value, 'current_requests') && hasNumber(value, 'admission_rejected') &&
    hasNumber(value, 'body_too_large') && hasString(value, 'metrics_started_at') && isRecord(value.udp) &&
    hasBoolean(value.udp, 'enabled') && hasString(value.udp, 'listener') && hasNumber(value.udp, 'sessions');
}

export function isConfigurationView(value: unknown): value is ConfigurationView {
  return isRecord(value) && hasString(value, 'config_path') && isRecord(value.effective) && hasString(value.effective, 'mode') &&
    isUDPConfiguration(value.effective.udp) &&
    isRecord(value.origins) && Array.isArray(value.overrides) && value.overrides.every((item) => typeof item === 'string') &&
    Array.isArray(value.pending_restart) && hasNumber(value, 'revision');
}

function isUDPConfiguration(value: unknown): boolean {
  return isRecord(value) && hasBoolean(value, 'enabled') && hasString(value, 'listen') && hasString(value, 'mode') &&
    hasString(value, 'upstream') && hasNumber(value, 'max_datagram_bytes') && hasNumber(value, 'max_sessions') &&
    hasNumber(value, 'session_ttl') && hasString(value, 'capture_response') && hasNumber(value, 'max_replies_per_second') &&
    hasNumber(value, 'max_reply_bytes_per_second');
}

export function isExchangeList(value: unknown): value is ExchangeList {
  return isRecord(value) && Array.isArray(value.items) && value.items.every(isExchangeSummary) && typeof value.has_more === 'boolean';
}

export function isExchangeDetail(value: unknown): value is ExchangeDetail {
  if (!isRecord(value) || !isRecord(value.exchange)) return false;
  const exchange = value.exchange;
  const shared = hasString(exchange, 'id') && hasString(exchange, 'transport') && hasString(exchange, 'mode') &&
    hasNumber(exchange, 'revision') && hasNumber(exchange, 'config_revision') && hasString(exchange, 'state') &&
    hasString(exchange, 'started_at') && hasNumber(exchange, 'duration_ns') &&
    typeof value.response_available === 'boolean';
  if (!shared) return false;
  if (exchange.transport === 'udp') {
    return isRecord(exchange.datagram) && hasString(exchange.datagram, 'source_address') &&
      hasString(exchange.datagram, 'local_address') && hasNumber(exchange.datagram, 'accepted_bytes') &&
      typeof exchange.datagram.payload_complete === 'boolean' && typeof exchange.datagram.preview_truncated === 'boolean' &&
      typeof value.datagram_available === 'boolean' && isRecord(exchange.datagram.delivery) && hasString(exchange.datagram.delivery, 'result') &&
      hasNumber(exchange.datagram.delivery, 'sent_bytes') && typeof exchange.datagram.delivery.truncated === 'boolean';
  }
  return exchange.transport === 'http' && typeof value.request_available === 'boolean' && isRecord(exchange.request);
}

export function assertObject<T>(value: unknown, name: string): asserts value is T {
  if (!isRecord(value)) throw new Error(`${name} response is invalid.`);
}
