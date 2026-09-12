export type OperatingMode = 'capture' | 'proxy';
export type ExchangeState = 'receiving' | 'forwarding' | 'completed' | 'failed' | 'cancelled' | 'discarded';

export interface SessionStatus {
  login_required: boolean;
  authenticated: boolean;
}

export interface StorageStatus {
  enabled: boolean;
  path: string;
  database_bytes: number;
  wal_bytes: number;
  exchange_count: number;
  queue_count: number;
  queue_capacity: number;
  page_size: number;
  page_count: number;
  free_pages: number;
  oldest_completed_at?: string;
  newest_completed_at?: string;
  reserved_count: number;
  reserved_bytes: number;
  queue_byte_capacity: number;
  writes: number;
  write_failures: number;
  busy_failures: number;
  last_write_latency_us: number;
  maintenance: string;
  maintenance_since?: string;
  last_error?: string;
  retention_days: number;
}

export interface RuntimeStatus {
  build: string;
  commit: string;
  build_date: string;
  mode: OperatingMode;
  management_listener: string;
  traffic_listener: string;
  uptime_seconds: number;
  ram_exchanges: number;
  ram_capacity: number;
  config_revision: number;
  current_requests: number;
  admission_rejected: number;
  body_too_large: number;
  metrics_started_at: string;
	udp: { enabled: boolean; listener: string; sessions: number };
  storage?: StorageStatus;
}

export interface EffectiveConfiguration {
  listeners: { management: string; traffic: string };
  mode: OperatingMode;
  upstream: { url: string; timeout: number };
  capture_response: { status: number; headers: Record<string, string[]>; body: string };
  cors: { enabled: boolean };
  limits: {
    request_body_bytes: number; response_body_bytes: number; request_header_bytes: number;
    response_header_bytes: number; concurrent_exchanges: number;
  };
  preview: { enabled: boolean; bytes: number };
  history: { max_exchanges: number };
  storage: { mode: 'memory' | 'sqlite'; sqlite_path: string; retention_days: number };
  authentication: { session_ttl: number };
  udp: {
    enabled: boolean;
    listen: string;
    mode: OperatingMode;
    upstream: string;
    max_datagram_bytes: number;
    max_sessions: number;
    session_ttl: number;
    capture_response: string;
    max_replies_per_second: number;
    max_reply_bytes_per_second: number;
  };
  shutdown_timeout: number;
}

export interface ConfigurationView {
  effective: EffectiveConfiguration;
  config_path: string;
  authentication_username?: string;
  authentication_configured?: boolean;
  origins: Record<string, string>;
  overrides: string[];
  pending_restart: string[];
  revision: number;
}

export interface ManagementReload {
  listener: string;
  address_changed: boolean;
  relogin_required: boolean;
}

export interface TrafficReload {
  listener: string;
  address_changed: boolean;
}

export interface UDPReload {
	enabled: boolean;
	listener: string;
	address_changed: boolean;
}

export type ConfigurationUpdateResponse = ConfigurationView & {
  management_reload?: ManagementReload;
  traffic_reload?: TrafficReload;
	udp_reload?: UDPReload;
};

export interface CleanupPreview {
  keep_days: number;
  cutoff: string;
  exchange_count: number;
  database_bytes: number;
}

export interface CleanupResult {
  cutoff: string;
  deleted: number;
}

export interface CompactResult {
  free_pages_before: number;
  free_pages_after: number;
  pages_reclaimed: number;
  bytes_reclaimed: number;
}

export interface ExchangeError {
  category: string;
  message: string;
}

interface ExchangeSummaryBase {
  id: string;
  mode: OperatingMode;
  state: ExchangeState;
  revision: number;
  config_revision: number;
  started_at: string;
  completed_at?: string;
  duration_ns: number;
  error?: ExchangeError;
}

export interface HTTPExchangeSummary extends ExchangeSummaryBase {
  transport?: 'http';
  method: string;
  path: string;
  request_body_bytes: number;
  response_body_bytes: number;
  response_status?: number;
  request_preview?: string;
  preview_truncated: boolean;
}

export interface UDPExchangeSummary extends ExchangeSummaryBase {
  transport: 'udp';
  source_address: string;
  local_address: string;
  datagram_bytes: number;
  discard_reason?: string;
}

export type ExchangeSummary = HTTPExchangeSummary | UDPExchangeSummary;

export interface ExchangeList {
  items: ExchangeSummary[];
  next_cursor?: string;
  has_more: boolean;
}

export interface CapturedRequest {
  method: string;
  scheme: string;
  protocol: string;
  host: string;
  request_target: string;
  path: string;
  raw_query: string;
  remote_address: string;
  headers: Record<string, string[]>;
  content_type: string;
  observed_body_bytes: number;
  body_complete: boolean;
  header_bytes_estimate: number;
  preview?: string;
  preview_truncated: boolean;
  decoded_preview_bytes?: number;
  preview_error?: string;
}

export interface CapturedResponse {
  status: number;
  headers: Record<string, string[]>;
  observed_body_bytes: number;
  body_complete: boolean;
  header_bytes_estimate: number;
  origin: 'upstream' | 'proxy_error';
  preview?: string;
  preview_truncated: boolean;
  decoded_preview_bytes?: number;
  preview_error?: string;
}

export interface ProxyTiming {
  upstream_started_at?: string;
  request_written_at?: string;
  first_response_byte_at?: string;
  response_captured_at?: string;
  downstream_completed_at?: string;
  upstream_dispatch_duration_us?: number;
  upstream_wait_duration_us?: number;
  upstream_receive_duration_us?: number;
  upstream_total_duration_us?: number;
  downstream_write_duration_us?: number;
  proxy_total_duration_us?: number;
}

interface ExchangeBase {
  id: string;
  mode: OperatingMode;
  config_revision: number;
  revision: number;
  state: ExchangeState;
  started_at: string;
  completed_at?: string;
  duration_ns: number;
  error?: ExchangeError;
  persistence?: { state: string; error?: string };
}

export interface HTTPExchange extends ExchangeBase {
  transport: 'http';
  request: CapturedRequest;
  response?: CapturedResponse;
  effective_upstream?: string;
  upstream_response_header_bytes_estimate?: number;
  upstream_observed_response_body_bytes?: number;
  proxy_timing?: ProxyTiming;
  downstream_delivery?: { status: number; body_bytes: number; error?: string };
}

export interface UDPDatagram {
  source_address: string;
  local_address: string;
  accepted_bytes: number;
  payload_complete: boolean;
  preview?: string;
  preview_truncated: boolean;
  discard_reason?: string;
  delivery: { result: UDPDeliveryResult; sent_bytes: number; message?: string; truncated: boolean };
}

export type UDPDeliveryResult = 'not_sent' | 'sent' | 'truncated' | 'discarded' | 'failed' | 'rate_limited';

export interface UDPExchange extends ExchangeBase {
  transport: 'udp';
  datagram: UDPDatagram;
}

export type Exchange = HTTPExchange | UDPExchange;

export interface ExchangeDetail {
  exchange: Exchange;
  request_available?: boolean;
  datagram_available?: boolean;
  response_available: boolean;
}

export const lifecycleEventTypes = [
  'exchange.started',
  'request.completed',
  'response.completed',
  'exchange.failed',
  'exchange.evicted',
  'exchange.deleted',
  'history.cleared',
  'config.changed'
] as const;

export type LifecycleEventType = (typeof lifecycleEventTypes)[number];

export interface LifecycleEnvelope {
  schema_version: 1;
  event_id: string;
  timestamp: string;
  type: LifecycleEventType;
  exchange_id?: string;
  revision: number;
  data?: Record<string, unknown>;
}

export interface StreamSnapshot {
  schema_version: 1;
  event_cursor: string;
  items: ExchangeSummary[];
}

export interface StreamResync extends StreamSnapshot {
  reason: 'replay_gap';
}

export interface APIErrorDocument {
  error: { code: string; message: string };
}
