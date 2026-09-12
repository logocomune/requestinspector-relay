import { assertObject } from './contract';
import type { APIErrorDocument, CleanupPreview, CleanupResult, CompactResult, ConfigurationUpdateResponse, ConfigurationView, ExchangeDetail, ExchangeList, OperatingMode, RuntimeStatus, SessionStatus, StorageStatus } from './types';

export class APIError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: 'same-origin',
    headers: init?.body ? { 'Content-Type': 'application/json', ...init.headers } : init?.headers
  });
  const body: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const error = body as APIErrorDocument | undefined;
    throw new APIError(response.status, error?.error?.code ?? 'request_failed', error?.error?.message ?? `Request failed (${response.status}).`);
  }
  assertObject<T>(body, path);
  return body;
}

export const api = {
  session: () => requestJSON<SessionStatus>('/session'),
  status: () => requestJSON<RuntimeStatus>('/status'),
  configuration: () => requestJSON<ConfigurationView>('/config'),
  exchanges: (cursor = '') => requestJSON<ExchangeList>(`/exchanges?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  exchange: (id: string) => requestJSON<ExchangeDetail>(`/exchanges/${encodeURIComponent(id)}`),
  deleteExchange: (id: string) => requestJSON<{ deleted: boolean }>(`/exchanges/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  clearExchanges: () => requestJSON<{ removed: number }>('/exchanges', { method: 'DELETE' }),
  body: (id: string, side: 'request' | 'response' | 'datagram', range?: string) => fetch(
    `/api/v1/exchanges/${encodeURIComponent(id)}/${side}/body`,
    { credentials: 'same-origin', headers: range ? { Range: range } : undefined }
  ),
  login: (username: string, password: string) => requestJSON<{ authenticated: boolean }>('/session', {
    method: 'POST', body: JSON.stringify({ username, password })
  }),
  logout: async () => {
    const response = await fetch('/api/v1/session', { method: 'DELETE', credentials: 'same-origin' });
    if (!response.ok) throw new APIError(response.status, 'logout_failed', 'Logout failed.');
  },
  setMode: (configuration: ConfigurationView, mode: OperatingMode) => requestJSON<ConfigurationView>('/config', {
    method: 'PUT',
    body: JSON.stringify({ expected_revision: configuration.revision, overrides: { mode } })
  }),
  updateConfiguration: (configuration: ConfigurationView, overrides: Record<string, unknown>, removeOverrides: string[] = []) => requestJSON<ConfigurationUpdateResponse>('/config', {
    method: 'PUT', body: JSON.stringify({ expected_revision: configuration.revision, overrides, remove_overrides: removeOverrides })
  }),
  removeAuthenticationOverrides: (configuration: ConfigurationView) => requestJSON<ConfigurationView>(`/config/overrides/authentication?expected_revision=${configuration.revision}`, { method: 'DELETE' }),
  removeOverride: (configuration: ConfigurationView, field: string) => requestJSON<ConfigurationView>(`/config/overrides/${encodeURIComponent(field)}?expected_revision=${configuration.revision}`, { method: 'DELETE' }),
  sqliteStatus: () => requestJSON<StorageStatus>('/storage/sqlite'),
  cleanupPreview: (keepDays: number) => requestJSON<CleanupPreview>(`/storage/sqlite/cleanup-preview?keep_days=${keepDays}`),
  cleanup: (keepDays: number) => requestJSON<CleanupResult>('/storage/sqlite/cleanup', {
    method: 'POST', body: JSON.stringify({ keep_days: keepDays, confirm: true })
  }),
  compact: () => requestJSON<CompactResult>('/storage/sqlite/compact', { method: 'POST' }),
  vacuum: () => requestJSON<{ completed: boolean }>('/storage/sqlite/vacuum', {
    method: 'POST', body: JSON.stringify({ confirm: true })
  })
};
