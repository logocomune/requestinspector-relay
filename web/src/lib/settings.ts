import type { ConfigurationView, OperatingMode } from './api/types';

export type SelectedTransport = 'http' | 'udp';
export const settingsTabs = ['HTTP', 'UDP', 'Storage', 'Maintenance', 'Administration'] as const;
export type SettingsTab = typeof settingsTabs[number];
export const httpSections = ['HTTP Capture', 'HTTP Proxy'] as const;
export type HTTPSection = typeof httpSections[number];
export const udpSections = ['UDP Capture', 'UDP Proxy'] as const;
export type UDPSection = typeof udpSections[number];
export type SettingsSection = HTTPSection | UDPSection;

export const selectedTransportStorageKey = 'reqrelay.selected-transport.v1';

export function selectedTransport(value: string | null | undefined): SelectedTransport {
  return value === 'udp' ? 'udp' : 'http';
}

export function udpTransportEnabled(enabled: boolean | undefined): boolean {
  return enabled === true;
}

export function udpFieldDisabled(path: string, enabled: boolean): boolean {
  return path.startsWith('udp.') && path !== 'udp.enabled' && !enabled;
}

export function udpTransportDisabledNotice(enabled: boolean): string | undefined {
  return enabled ? undefined : 'UDP disabled.';
}

export function managementDisplayURL(listener: string, currentHostname: string): string | undefined {
  const parsed = parseListener(listener);
  if (!parsed || parsed.port === '0') return undefined;
  const hostname = wildcardHost(parsed.hostname) ? currentHostname : parsed.hostname;
  const formattedHostname = hostname.includes(':') ? `[${hostname}]` : hostname;
  return `http://${formattedHostname}:${parsed.port}/settings`;
}

export interface FormattedEndpoint {
  url?: string;
  address: string;
  copyValue: string;
  boundAddress: string;
  port: string;
  isWildcard: boolean;
}

export function formatEndpoint(
  protocol: 'http' | 'udp',
  listener: string | undefined,
  currentHostname: string = 'localhost'
): FormattedEndpoint | undefined {
  if (!listener) return undefined;
  const parsed = parseListener(listener);
  if (!parsed) {
    const isHTTP = protocol === 'http';
    const cleanURL = isHTTP ? (listener.startsWith('http') ? listener : `http://${listener}`) : undefined;
    return {
      url: cleanURL,
      address: listener,
      copyValue: cleanURL ?? listener,
      boundAddress: listener,
      port: '',
      isWildcard: false
    };
  }

  const isWildcard = wildcardHost(parsed.hostname);
  const effectiveHost = isWildcard
    ? (currentHostname && !wildcardHost(currentHostname) ? currentHostname : 'localhost')
    : parsed.hostname;

  const formattedHost = effectiveHost.includes(':') ? `[${effectiveHost}]` : effectiveHost;
  const address = `${formattedHost}:${parsed.port}`;
  const url = protocol === 'http' ? `http://${address}` : undefined;

  return {
    url,
    address,
    copyValue: url ?? address,
    boundAddress: listener,
    port: parsed.port,
    isWildcard
  };
}

interface ManagementNavigation {
  current: URL;
  previousListener: string;
  nextListener: string;
}

export function managementNavigationURL({ current, previousListener, nextListener }: ManagementNavigation): string | undefined {
  if (current.protocol !== 'http:') return undefined;
  const previous = parseListener(previousListener);
  const next = parseListener(nextListener);
  if (!previous || !next || next.port === '0') return undefined;
  const currentPort = current.port || '80';
  if (previous.port !== currentPort || (!wildcardHost(previous.hostname) && !equivalentHost(previous.hostname, current.hostname))) return undefined;
  if (!wildcardHost(next.hostname) && !equivalentHost(next.hostname, current.hostname)) return undefined;
  const target = new URL(current.toString());
  target.port = next.port;
  target.pathname = '/settings';
  target.search = '';
  target.hash = '';
  return target.toString();
}

function parseListener(listener: string): { hostname: string; port: string } | undefined {
  const normalized = listener.startsWith(':') ? `0.0.0.0${listener}` : listener;
  try {
    const parsed = new URL(`http://${normalized}`);
    const port = parsed.port || '80';
    return { hostname: parsed.hostname.replace(/^\[|\]$/g, '').toLowerCase(), port };
  } catch {
    return undefined;
  }
}

function wildcardHost(hostname: string): boolean {
  return hostname === '' || hostname === '0.0.0.0' || hostname === '::';
}

function equivalentHost(left: string, right: string): boolean {
  const normalizedLeft = left.replace(/^\[|\]$/g, '').toLowerCase();
  const normalizedRight = right.replace(/^\[|\]$/g, '').toLowerCase();
  if (normalizedLeft === normalizedRight) return true;
  return loopbackHost(normalizedLeft) && loopbackHost(normalizedRight);
}

function loopbackHost(hostname: string): boolean {
  return hostname === 'localhost' || hostname === '::1' || hostname.startsWith('127.');
}

export type FieldKind = 'text' | 'textarea' | 'integer' | 'boolean' | 'duration' | 'json' | 'select' | 'password';

export interface SelectOption {
  value: string;
  label: string;
}

export interface ConfigurationField {
  path: string;
  label: string;
  tab: SettingsTab;
  section?: SettingsSection;
  kind: FieldKind;
  valueType?: 'string' | 'integer';
  options?: Array<string | SelectOption>;
  help: string;
}

export const httpStatusOptions: SelectOption[] = [
  { value: '200', label: '200 OK - Request succeeded' },
  { value: '201', label: '201 Created - Resource created' },
  { value: '202', label: '202 Accepted - Accepted for processing' },
  { value: '204', label: '204 No Content - Success with no body' },
  { value: '301', label: '301 Moved Permanently - Resource moved permanently' },
  { value: '302', label: '302 Found - Temporary redirect' },
  { value: '304', label: '304 Not Modified - Cached copy still valid' },
  { value: '307', label: '307 Temporary Redirect - Redirect with same HTTP method' },
  { value: '308', label: '308 Permanent Redirect - Permanent redirect with same method' },
  { value: '400', label: '400 Bad Request - Malformed request syntax' },
  { value: '401', label: '401 Unauthorized - Authentication required' },
  { value: '403', label: '403 Forbidden - Access denied' },
  { value: '404', label: '404 Not Found - Resource not found' },
  { value: '405', label: '405 Method Not Allowed - HTTP method not supported' },
  { value: '408', label: '408 Request Timeout - Client timed out' },
  { value: '409', label: '409 Conflict - Request conflicts with current state' },
  { value: '410', label: '410 Gone - Resource permanently deleted' },
  { value: '413', label: '413 Payload Too Large - Request entity too large' },
  { value: '415', label: '415 Unsupported Media Type - Unsupported payload format' },
  { value: '418', label: "418 I'm a teapot - Server refuses coffee" },
  { value: '422', label: '422 Unprocessable Content - Semantic validation error' },
  { value: '429', label: '429 Too Many Requests - Rate limit exceeded' },
  { value: '500', label: '500 Internal Server Error - Unexpected server failure' },
  { value: '501', label: '501 Not Implemented - Server functionality missing' },
  { value: '502', label: '502 Bad Gateway - Invalid upstream response' },
  { value: '503', label: '503 Service Unavailable - Server overloaded or maintenance' },
  { value: '504', label: '504 Gateway Timeout - Upstream server timed out' }
];

export const configurationFields: ConfigurationField[] = [
  { path: 'mode', label: 'Operating mode', tab: 'HTTP', kind: 'select', options: ['capture', 'proxy'], help: 'Choose how new HTTP requests are handled; active requests keep their existing mode.' },
  { path: 'listeners.traffic', label: 'Traffic listener', tab: 'HTTP', kind: 'text', help: 'HTTP host and port accepting inspected traffic. Saving hot-reloads the listener on the new address.' },
  { path: 'limits.request_header_bytes', label: 'Request header bytes', tab: 'HTTP', kind: 'integer', help: 'Maximum normalized request-header size accepted from each HTTP client, in bytes.' },
  { path: 'limits.request_body_bytes', label: 'Request body bytes', tab: 'HTTP', kind: 'integer', help: 'Maximum request-body size accepted from each HTTP client, in bytes.' },
  { path: 'cors.enabled', label: 'Browser CORS', tab: 'HTTP', kind: 'boolean', help: 'Allow browser clients from other origins to call HTTP ingest; the request Origin is reflected, preflight returns 204, and credentials remain disabled.' },
  { path: 'capture_response.status', label: 'Capture status', tab: 'HTTP', section: 'HTTP Capture', kind: 'select', valueType: 'integer', options: httpStatusOptions, help: 'HTTP status returned immediately when Capture mode accepts a request.' },
  { path: 'capture_response.headers', label: 'Capture headers', tab: 'HTTP', section: 'HTTP Capture', kind: 'json', help: 'Response headers sent in Capture mode as a JSON object of header-name arrays.' },
  { path: 'capture_response.body', label: 'Capture body', tab: 'HTTP', section: 'HTTP Capture', kind: 'textarea', help: 'Response body returned immediately when Capture mode accepts a request.' },
  { path: 'upstream.url', label: 'Upstream URL', tab: 'HTTP', section: 'HTTP Proxy', kind: 'text', help: 'Absolute HTTP or HTTPS destination used for forwarded Proxy-mode requests.' },
  { path: 'upstream.timeout', label: 'Upstream timeout', tab: 'HTTP', section: 'HTTP Proxy', kind: 'duration', help: 'Maximum time RequestInspector Relay waits for upstream connection and response, for example 30s.' },
  { path: 'limits.response_header_bytes', label: 'Response header bytes', tab: 'HTTP', section: 'HTTP Proxy', kind: 'integer', help: 'Maximum normalized response-header size accepted from the HTTP upstream, in bytes.' },
  { path: 'limits.response_body_bytes', label: 'Response body bytes', tab: 'HTTP', section: 'HTTP Proxy', kind: 'integer', help: 'Maximum response-body size accepted from the HTTP upstream, in bytes.' },
  { path: 'limits.concurrent_exchanges', label: 'Concurrent exchanges', tab: 'HTTP', section: 'HTTP Proxy', kind: 'integer', help: 'Maximum HTTP exchanges processed at once; excess requests are rejected safely.' },
  { path: 'udp.enabled', label: 'Enabled', tab: 'UDP', kind: 'boolean', help: 'Starts or stops UDP ingest when settings are saved; disabled UDP owns no socket.' },
  { path: 'udp.mode', label: 'Mode', tab: 'UDP', kind: 'select', options: ['capture', 'proxy'], help: 'Choose whether UDP datagrams are recorded only or forwarded to the fixed upstream. Saving hot-reloads UDP ingest.' },
  { path: 'udp.listen', label: 'Listener', tab: 'UDP', kind: 'text', help: 'UDP host and port receiving datagrams. Saving binds and activates the new address.' },
  { path: 'udp.max_datagram_bytes', label: 'Maximum datagram bytes', tab: 'UDP', kind: 'integer', help: 'Largest accepted UDP datagram, from 1 to 65507 bytes; larger datagrams are discarded.' },
  { path: 'udp.capture_response', label: 'Capture reply', tab: 'UDP', section: 'UDP Capture', kind: 'textarea', help: 'Optional reply sent to each accepted UDP client in Capture mode; empty sends no reply.' },
  { path: 'udp.upstream', label: 'Proxy upstream', tab: 'UDP', section: 'UDP Proxy', kind: 'text', help: 'Fixed unicast host and port receiving UDP Proxy datagrams after settings are saved.' },
  { path: 'udp.max_sessions', label: 'Maximum sessions', tab: 'UDP', section: 'UDP Proxy', kind: 'integer', help: 'Maximum active client-to-upstream UDP mappings, from 1 to 4096; oldest mappings may expire.' },
  { path: 'udp.session_ttl', label: 'Session TTL', tab: 'UDP', section: 'UDP Proxy', kind: 'duration', help: 'Idle lifetime of each UDP client mapping, up to 30s, before its upstream socket is closed.' },
  { path: 'udp.max_replies_per_second', label: 'Reply rate', tab: 'UDP', section: 'UDP Proxy', kind: 'integer', help: 'Maximum upstream replies per second delivered to each UDP client mapping.' },
  { path: 'udp.max_reply_bytes_per_second', label: 'Reply byte rate', tab: 'UDP', section: 'UDP Proxy', kind: 'integer', help: 'Maximum upstream reply bytes per second delivered to each UDP client mapping.' },
  { path: 'preview.enabled', label: 'Bound previews', tab: 'Storage', kind: 'boolean', help: 'Limits displayed payload previews while retaining the full captured payload for download.' },
  { path: 'preview.bytes', label: 'Preview bytes', tab: 'Storage', kind: 'integer', help: 'Maximum bytes displayed for each payload preview when bounded previews are enabled.' },
  { path: 'history.max_exchanges', label: 'RAM exchanges', tab: 'Storage', kind: 'integer', help: 'Maximum completed exchanges retained in memory before oldest entries are evicted.' },
  { path: 'storage.mode', label: 'Storage mode', tab: 'Storage', kind: 'select', options: ['memory', 'sqlite'], help: 'Choose in-memory history or durable SQLite history. Save, then restart to apply it.' },
  { path: 'storage.sqlite_path', label: 'SQLite path', tab: 'Storage', kind: 'text', help: 'Filesystem path for durable SQLite history. Save, then restart to open the new database.' },
  { path: 'storage.retention_days', label: 'Retention days', tab: 'Storage', kind: 'integer', help: 'Completed SQLite exchanges older than this many days are cleaned up; zero keeps them indefinitely.' },
  { path: 'listeners.management', label: 'Management listener', tab: 'Administration', kind: 'text', help: 'HTTP host and port serving this dashboard and management API. Saving binds the new address without stopping inspected traffic.' },
	{ path: 'authentication.session_ttl', label: 'Session lifetime', tab: 'Administration', kind: 'duration', help: 'How long an authenticated dashboard session remains valid. Saving reloads management sessions.' },
  { path: 'authentication.username', label: 'Username', tab: 'Administration', kind: 'text', help: 'Dashboard login username. Saving reloads authentication and invalidates existing sessions.' },
  { path: 'authentication.password', label: 'Password', tab: 'Administration', kind: 'password', help: 'Write-only dashboard password. Saving reloads authentication and invalidates existing sessions.' },
  { path: 'shutdown_timeout', label: 'Shutdown timeout', tab: 'Administration', kind: 'duration', help: 'Maximum graceful-shutdown time allowed for active traffic and pending SQLite writes.' }
];

export function draftConfiguration(view: ConfigurationView): Record<string, string> {
  const drafts: Record<string, string> = {};
  for (const field of configurationFields) {
    if (field.path === 'authentication.username') {
      drafts[field.path] = view.authentication_username ?? '';
      continue;
    }
    if (field.path === 'authentication.password') {
      drafts[field.path] = view.authentication_configured ? '*****' : '';
      continue;
    }
    const value = valueAt(view.effective as unknown as Record<string, unknown>, field.path);
    drafts[field.path] = serializeValue(field.kind, value);
  }
  return drafts;
}

export function normalizeSelectOptions(options?: Array<string | SelectOption>, currentValue?: string): SelectOption[] {
  if (!options) return [];
  const normalized: SelectOption[] = options.map((opt) => typeof opt === 'string' ? { value: opt, label: opt } : opt);
  if (currentValue && !normalized.some((opt) => opt.value === currentValue)) {
    return [{ value: currentValue, label: `${currentValue} - Custom status` }, ...normalized];
  }
  return normalized;
}

export function parseDrafts(drafts: Record<string, string>, dirty: Set<string>): {
  overrides: Record<string, unknown>;
  errors: Record<string, string>;
} {
  const overrides: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const field of configurationFields) {
    if (!dirty.has(field.path)) continue;
    const raw = drafts[field.path] ?? '';
    try {
      overrides[field.path] = parseValue(field, raw);
    } catch {
      errors[field.path] = field.kind === 'json' ? 'Enter valid JSON.' : field.kind === 'integer' || field.valueType === 'integer' ? 'Enter a valid integer.' : `Enter a valid ${field.kind}.`;
    }
  }
  return { overrides, errors };
}

export function normalizeAuthenticationOverrides(
  drafts: Record<string, string>,
  dirty: Set<string>,
  overrides: Record<string, unknown>
): Record<string, unknown> {
  const usernameCleared = dirty.has('authentication.username') && drafts['authentication.username'] === '';
  const passwordCleared = dirty.has('authentication.password') && drafts['authentication.password'] === '';
  if (!usernameCleared && !passwordCleared) return overrides;
  return { ...overrides, 'authentication.username': '', 'authentication.password': '' };
}

export function modeSwitchError(mode: OperatingMode, upstreamURL: string): string {
  return mode === 'proxy' && upstreamURL.trim() === '' ? 'Upstream URL is required for proxy mode.' : '';
}

export function udpModeSwitchError(mode: OperatingMode, upstream: string): string {
  return mode === 'proxy' && upstream.trim() === '' ? 'UDP upstream is required for proxy mode.' : '';
}

export function udpModeSwitchNotice(mode: OperatingMode): string {
	return `UDP mode changed to ${mode}. UDP ingest reloaded.`;
}

export function modeSwitchNotice(mode: OperatingMode, activeExchanges: number): string {
  const activeNotice = activeExchanges > 0 ? ` ${activeExchanges} active exchange(s) keep their current mode.` : '';
  return `Operating mode changed to ${mode}. New requests use ${mode} mode.${activeNotice}`;
}

function valueAt(root: Record<string, unknown>, path: string): unknown {
  let value: unknown = root;
  for (const part of path.split('.')) value = (value as Record<string, unknown>)[part];
  return value;
}

function serializeValue(kind: FieldKind, value: unknown): string {
  if (kind === 'duration') return formatDuration(Number(value));
  if (kind === 'json') return JSON.stringify(value, null, 2);
  return String(value ?? '');
}

function parseValue(field: ConfigurationField, raw: string): unknown {
  if (field.kind === 'integer' || (field.kind === 'select' && field.valueType === 'integer')) {
    const value = Number(raw);
    if (!Number.isSafeInteger(value)) throw new Error('invalid integer');
    return value;
  }
  if (field.kind === 'boolean') {
    if (raw !== 'true' && raw !== 'false') throw new Error('invalid boolean');
    return raw === 'true';
  }
  if (field.kind === 'json') return JSON.parse(raw);
  if (field.kind === 'duration' && !/^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)$/.test(raw)) throw new Error('invalid duration');
  return raw;
}

function formatDuration(nanoseconds: number): string {
  const units: Array<[number, string]> = [[3_600_000_000_000, 'h'], [60_000_000_000, 'm'], [1_000_000_000, 's'], [1_000_000, 'ms'], [1_000, 'us']];
  for (const [size, suffix] of units) if (nanoseconds % size === 0) return `${nanoseconds / size}${suffix}`;
  return `${nanoseconds}ns`;
}
