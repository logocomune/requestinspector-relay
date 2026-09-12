import { describe, expect, it } from 'vitest';
import { exchangeTransport, filterByTransport } from './transport-filter';
import type { ExchangeSummary } from '$lib/api/types';

const http: ExchangeSummary = {
  id: 'http', mode: 'capture', state: 'completed', revision: 1, config_revision: 1,
  started_at: '2026-09-09T10:00:00Z', duration_ns: 0, method: 'GET', path: '/',
  request_body_bytes: 0, response_body_bytes: 0, preview_truncated: false
};
const udp: ExchangeSummary = {
  id: 'udp', transport: 'udp', mode: 'proxy', state: 'completed', revision: 1, config_revision: 1,
  started_at: '2026-09-09T10:00:01Z', duration_ns: 0, source_address: '127.0.0.1:40000',
  local_address: '127.0.0.1:9000', datagram_bytes: 4
};

describe('transport filtering', () => {
  it('normalizes legacy HTTP summaries and preserves order', () => {
    expect(exchangeTransport(http)).toBe('http');
    expect(exchangeTransport(udp)).toBe('udp');
    expect(filterByTransport([udp, http], 'all')).toEqual([udp, http]);
    expect(filterByTransport([udp, http], 'http')).toEqual([http]);
    expect(filterByTransport([udp, http], 'udp')).toEqual([udp]);
  });
});
