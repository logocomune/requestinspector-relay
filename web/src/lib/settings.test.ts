import { describe, expect, it } from 'vitest';
import fc from 'fast-check';
import { configurationFields, draftConfiguration, formatEndpoint, managementDisplayURL, managementNavigationURL, modeSwitchError, modeSwitchNotice, normalizeAuthenticationOverrides, normalizeSelectOptions, parseDrafts, selectedTransport, settingsTabs, udpFieldDisabled, udpModeSwitchError, udpModeSwitchNotice, udpTransportDisabledNotice, udpTransportEnabled } from './settings';
import type { ConfigurationView } from './api/types';

const configuration: ConfigurationView = {
  effective: {
    listeners: { management: '127.0.0.1:8080', traffic: '0.0.0.0:8081' },
    mode: 'capture', upstream: { url: '', timeout: 30_000_000_000 },
    capture_response: { status: 200, headers: { 'Content-Type': ['application/json'] }, body: '{"ok":true}' },
    cors: { enabled: false },
    limits: { request_body_bytes: 1024, response_body_bytes: 2048, request_header_bytes: 4096, response_header_bytes: 4096, concurrent_exchanges: 8 },
    preview: { enabled: true, bytes: 256 }, history: { max_exchanges: 100 },
    storage: { mode: 'sqlite', sqlite_path: '/tmp/reqrelay.db', retention_days: 7 },
    authentication: { session_ttl: 86_400_000_000_000 },
    udp: { enabled: false, listen: '0.0.0.0:12050', mode: 'capture', upstream: '', max_datagram_bytes: 65507, max_sessions: 1024, session_ttl: 30_000_000_000, capture_response: '', max_replies_per_second: 100, max_reply_bytes_per_second: 1_048_576 },
    shutdown_timeout: 10_000_000_000
  },
  config_path: '/home/test/.config/requestinspector-relay/reqrelay.yaml', origins: {}, overrides: [], pending_restart: [], revision: 1
};

describe('settings drafts', () => {
	it('gives every setting an operator-facing explanation', () => {
		for (const field of configurationFields) {
			expect(field.help).not.toBe('Restart required.');
			expect(field.help.length).toBeGreaterThanOrEqual(40);
		}
	});

	 it('groups HTTP settings under one tab with capture and proxy sections', () => {
		 expect(settingsTabs).toEqual(['HTTP', 'UDP', 'Storage', 'Maintenance', 'Administration']);
		 expect(configurationFields.every((field) => settingsTabs.includes(field.tab))).toBe(true);
		 expect(configurationFields.find((field) => field.path === 'mode')).toMatchObject({ tab: 'HTTP' });
		 expect(configurationFields.find((field) => field.path === 'mode')?.section).toBeUndefined();
		 expect(configurationFields.find((field) => field.path === 'listeners.traffic')).toMatchObject({ tab: 'HTTP' });
		 expect(configurationFields.find((field) => field.path === 'listeners.traffic')?.section).toBeUndefined();
		 for (const path of ['limits.request_body_bytes', 'limits.request_header_bytes']) {
			 expect(configurationFields.find((field) => field.path === path)).toMatchObject({ tab: 'HTTP' });
			expect(configurationFields.find((field) => field.path === path)?.section).toBeUndefined();
		 }
		expect(configurationFields.filter((field) => field.tab === 'HTTP').slice(0, 4).map((field) => field.label)).toEqual(['Operating mode', 'Traffic listener', 'Request header bytes', 'Request body bytes']);
		expect(configurationFields.filter((field) => field.section === 'HTTP Proxy').map((field) => field.label)).toEqual(['Upstream URL', 'Upstream timeout', 'Response header bytes', 'Response body bytes', 'Concurrent exchanges']);
		 expect(configurationFields.find((field) => field.path === 'capture_response.body')).toMatchObject({ tab: 'HTTP', section: 'HTTP Capture' });
		 expect(configurationFields.find((field) => field.path === 'upstream.url')).toMatchObject({ tab: 'HTTP', section: 'HTTP Proxy' });
		 expect(configurationFields.find((field) => field.path === 'udp.capture_response')).toMatchObject({ tab: 'UDP', section: 'UDP Capture' });
		 expect(configurationFields.find((field) => field.path === 'udp.upstream')).toMatchObject({ tab: 'UDP', section: 'UDP Proxy' });
		 expect(configurationFields.filter((field) => field.tab === 'UDP').slice(0, 4).map((field) => field.label)).toEqual(['Enabled', 'Mode', 'Listener', 'Maximum datagram bytes']);
		 for (const path of ['udp.enabled', 'udp.listen', 'udp.mode', 'udp.max_datagram_bytes']) {
			 expect(configurationFields.find((field) => field.path === path)).toMatchObject({ tab: 'UDP' });
			 expect(configurationFields.find((field) => field.path === path)?.section).toBeUndefined();
		 }
	 });

	it('does not show restart guidance in the HTTP tab', () => {
		for (const field of configurationFields.filter((field) => field.tab === 'HTTP')) {
			expect(field.help).not.toMatch(/restart/i);
		}
	});

  it('places session lifetime beside management listener before username and password', () => {
    expect(configurationFields.filter((field) => field.tab === 'Administration').map((field) => field.path)).toEqual([
      'listeners.management', 'authentication.session_ttl', 'authentication.username', 'authentication.password', 'shutdown_timeout'
    ]);
  });

  it('navigates direct HTTP sessions after a management port reload', () => {
    expect(managementNavigationURL({ current: new URL('http://localhost:8080/settings'), previousListener: '127.0.0.1:8080', nextListener: '127.0.0.1:9080' })).toBe('http://localhost:9080/settings');
    expect(managementNavigationURL({ current: new URL('https://example.test/settings'), previousListener: '127.0.0.1:8080', nextListener: '127.0.0.1:9080' })).toBeUndefined();
    expect(managementNavigationURL({ current: new URL('http://proxy.test/settings'), previousListener: '127.0.0.1:8080', nextListener: '127.0.0.1:9080' })).toBeUndefined();
    expect(managementNavigationURL({ current: new URL('http://127.0.0.1:8080/settings'), previousListener: '127.0.0.1:8080', nextListener: '192.0.2.10:9080' })).toBeUndefined();
    expect(managementDisplayURL('0.0.0.0:9080', 'reqrelay.local')).toBe('http://reqrelay.local:9080/settings');
    expect(managementDisplayURL('[::1]:9080', 'localhost')).toBe('http://[::1]:9080/settings');
  });

  it('formats endpoints cleanly without awkward udp:// or IPv6 wildcard brackets', () => {
    // IPv6 wildcard UDP listener [::]:1799 resolved to localhost
    const udpIPv6 = formatEndpoint('udp', '[::]:1799', 'localhost');
    expect(udpIPv6).toEqual({
      url: undefined,
      address: 'localhost:1799',
      copyValue: 'localhost:1799',
      boundAddress: '[::]:1799',
      port: '1799',
      isWildcard: true
    });

    // IPv4 wildcard UDP listener 0.0.0.0:12050
    const udpIPv4 = formatEndpoint('udp', '0.0.0.0:12050', 'localhost');
    expect(udpIPv4?.address).toBe('localhost:12050');
    expect(udpIPv4?.copyValue).toBe('localhost:12050');
    expect(udpIPv4?.isWildcard).toBe(true);

    // Explicit IP UDP listener 127.0.0.1:12050
    const udpExplicit = formatEndpoint('udp', '127.0.0.1:12050', 'localhost');
    expect(udpExplicit?.address).toBe('127.0.0.1:12050');
    expect(udpExplicit?.copyValue).toBe('127.0.0.1:12050');
    expect(udpExplicit?.isWildcard).toBe(false);

    // HTTP wildcard listener 0.0.0.0:8081
    const httpWildcard = formatEndpoint('http', '0.0.0.0:8081', 'localhost');
    expect(httpWildcard?.url).toBe('http://localhost:8081');
    expect(httpWildcard?.copyValue).toBe('http://localhost:8081');
    expect(httpWildcard?.isWildcard).toBe(true);

    // Remote hostname preservation
    const remoteUdp = formatEndpoint('udp', '[::]:1799', 'reqrelay.lan');
    expect(remoteUdp?.address).toBe('reqrelay.lan:1799');
    expect(remoteUdp?.copyValue).toBe('reqrelay.lan:1799');

    // Missing listener returns undefined
    expect(formatEndpoint('udp', undefined)).toBeUndefined();
  });

  it('maps arbitrary direct loopback port changes without preserving query data', () => {
    fc.assert(fc.property(fc.integer({ min: 1024, max: 65535 }), (port) => {
      const target = managementNavigationURL({ current: new URL('http://localhost:8080/settings?secret=value#fragment'), previousListener: '127.0.0.1:8080', nextListener: `127.0.0.1:${port}` });
      expect(target).toBe(`http://localhost:${port}/settings`);
    }), { seed: 20260910, numRuns: 300 });
  });

	it('keeps HTTP and UDP selection independent and validates UDP proxy mode', () => {
		 expect(selectedTransport('udp')).toBe('udp');
		 expect(selectedTransport('http')).toBe('http');
		 expect(selectedTransport('invalid')).toBe('http');
		 expect(udpModeSwitchError('proxy', '  ')).toBe('UDP upstream is required for proxy mode.');
		 expect(udpModeSwitchError('capture', '')).toBe('');
		 expect(udpModeSwitchNotice('proxy')).toBe('UDP mode changed to proxy. UDP ingest reloaded.');
	});

	it('enables transport control only when UDP listener runs', () => {
		expect(udpTransportEnabled(false)).toBe(false);
		expect(udpTransportEnabled(true)).toBe(true);
		expect(udpTransportEnabled(undefined)).toBe(false);
	});

	it('disables UDP fields except Enabled when UDP is off', () => {
		expect(udpFieldDisabled('udp.listen', false)).toBe(true);
		expect(udpFieldDisabled('udp.capture_response', false)).toBe(true);
		expect(udpFieldDisabled('udp.listen', true)).toBe(false);
		expect(udpFieldDisabled('udp.enabled', false)).toBe(false);
		expect(udpFieldDisabled('listeners.traffic', false)).toBe(false);
	});

	it('returns toast notice when selecting disabled UDP transport', () => {
		expect(udpTransportDisabledNotice(false)).toBe('UDP disabled.');
		expect(udpTransportDisabledNotice(true)).toBeUndefined();
	});

	it('does not show restart guidance in the UDP tab', () => {
		for (const field of configurationFields.filter((field) => field.tab === 'UDP')) {
			expect(field.help).not.toMatch(/restart/i);
		}
	});

  it('serializes effective values without exposing authentication secrets', () => {
    const drafts = draftConfiguration(configuration);
    expect(drafts['upstream.timeout']).toBe('30s');
    expect(drafts['authentication.username']).toBe('');
    expect(drafts['authentication.password']).toBe('');
	 expect(drafts['capture_response.headers']).toContain('Content-Type');
	expect(drafts['udp.max_sessions']).toBe('1024');
	const configured = { ...configuration, authentication_username: 'operator', authentication_configured: true };
	const configuredDrafts = draftConfiguration(configured);
	expect(configuredDrafts['authentication.username']).toBe('operator');
	expect(configuredDrafts['authentication.password']).toBe('*****');
  });

  it('parses typed overrides and reports invalid fields', () => {
    const drafts = draftConfiguration(configuration);
    drafts['preview.enabled'] = 'false';
    drafts['history.max_exchanges'] = '0';
    drafts['capture_response.headers'] = '{bad';
    const parsed = parseDrafts(drafts, new Set(['preview.enabled', 'history.max_exchanges', 'capture_response.headers']));
    expect(parsed.overrides['preview.enabled']).toBe(false);
    expect(parsed.overrides['history.max_exchanges']).toBe(0);
    expect(parsed.errors).toEqual({ 'capture_response.headers': 'Enter valid JSON.' });
  });

  it('normalizes authentication credential clears into one atomic override', () => {
    const cases = [
      { drafts: { 'authentication.username': '', 'authentication.password': '*****' }, dirty: ['authentication.username'], input: { 'authentication.username': '' }, want: { 'authentication.username': '', 'authentication.password': '' } },
      { drafts: { 'authentication.username': 'operator', 'authentication.password': '' }, dirty: ['authentication.password'], input: { 'authentication.password': '' }, want: { 'authentication.username': '', 'authentication.password': '' } },
      { drafts: { 'authentication.username': 'operator', 'authentication.password': 'secret' }, dirty: ['authentication.username'], input: { 'authentication.username': 'operator' }, want: { 'authentication.username': 'operator' } }
    ];
    for (const testCase of cases) {
      expect(normalizeAuthenticationOverrides(testCase.drafts, new Set(testCase.dirty), testCase.input)).toEqual(testCase.want);
    }
  });

  it('round-trips safe integers through every integer field', () => {
    const integerFields = configurationFields.filter((field) => field.kind === 'integer' || field.valueType === 'integer');
    fc.assert(fc.property(fc.integer({ min: 0, max: 1_000_000 }), (value) => {
      for (const field of integerFields) {
        const parsed = parseDrafts({ [field.path]: String(value) }, new Set([field.path]));
        expect(parsed.errors).toEqual({});
        expect(parsed.overrides[field.path]).toBe(value);
      }
    }), { seed: 20260907, numRuns: 300 });
  });

  it('provides comprehensive HTTP status options and parses capture status as integer', () => {
    const statusField = configurationFields.find((field) => field.path === 'capture_response.status');
    expect(statusField?.kind).toBe('select');
    expect(statusField?.valueType).toBe('integer');
    expect(statusField?.options).toBeDefined();

    const options = statusField?.options ?? [];
    const values = options.map((opt) => typeof opt === 'string' ? opt : opt.value);
    expect(values).toContain('200');
    expect(values).toContain('404');
    expect(values).toContain('500');

    const normalized = normalizeSelectOptions(options, '499');
    expect(normalized[0]).toEqual({ value: '499', label: '499 - Custom status' });
    expect(normalizeSelectOptions(['a', 'b'])).toEqual([{ value: 'a', label: 'a' }, { value: 'b', label: 'b' }]);

    const parsed = parseDrafts({ 'capture_response.status': '404' }, new Set(['capture_response.status']));
    expect(parsed.errors).toEqual({});
    expect(parsed.overrides['capture_response.status']).toBe(404);
  });

  it('guards proxy target and explains active exchange mode snapshots', () => {
    expect(modeSwitchError('proxy', '  ')).toBe('Upstream URL is required for proxy mode.');
    expect(modeSwitchError('capture', '')).toBe('');
    expect(modeSwitchError('proxy', 'https://example.test')).toBe('');
    expect(modeSwitchNotice('capture', 0)).toBe('Operating mode changed to capture. New requests use capture mode.');
    expect(modeSwitchNotice('proxy', 2)).toContain('2 active exchange(s) keep their current mode.');
  });

  it('never throws for arbitrary user-entered duration or JSON text', () => {
    fc.assert(fc.property(fc.string(), (raw) => {
      const parsed = parseDrafts({ 'upstream.timeout': raw, 'capture_response.headers': raw }, new Set(['upstream.timeout', 'capture_response.headers']));
      expect(Object.keys(parsed.errors).length + Object.keys(parsed.overrides).length).toBe(2);
    }), { seed: 20260908, numRuns: 500 });
  });

  it('preserves arbitrary JSON values accepted by the editor', () => {
    fc.assert(fc.property(fc.jsonValue(), (value) => {
      const parsed = parseDrafts({ 'capture_response.headers': JSON.stringify(value) }, new Set(['capture_response.headers']));
      expect(parsed.errors).toEqual({});
      expect(JSON.stringify(parsed.overrides['capture_response.headers'])).toBe(JSON.stringify(value));
    }), { seed: 20260909, numRuns: 300 });
  });

  it('accepts JSON negative zero using standard JSON normalization', () => {
    const parsed = parseDrafts({ 'capture_response.headers': JSON.stringify({ '': -0 }) }, new Set(['capture_response.headers']));
    expect(parsed.errors).toEqual({});
    expect(parsed.overrides['capture_response.headers']).toEqual({ '': 0 });
  });
});
