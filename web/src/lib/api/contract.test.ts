import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { isConfigurationView, isExchangeDetail, isExchangeList, isLifecycleEnvelope, isRuntimeStatus, isStreamSnapshot } from './contract';

const fixture = (name: string): unknown => JSON.parse(readFileSync(new URL(`../../../../docs/api-v1-fixtures/${name}`, import.meta.url), 'utf8'));

describe('frozen API v1 fixtures', () => {
  it('matches frontend runtime guards', () => {
    expect(isRuntimeStatus(fixture('status.json'))).toBe(true);
    expect(isConfigurationView(fixture('config.json'))).toBe(true);
    expect(isExchangeList(fixture('exchanges.json'))).toBe(true);
    expect(isExchangeDetail(fixture('exchange-detail.json'))).toBe(true);
    expect(isExchangeDetail(fixture('udp-exchange-detail.json'))).toBe(true);
    expect(isStreamSnapshot(fixture('sse-snapshot.json'))).toBe(true);
    expect(isStreamSnapshot(fixture('sse-resync.json'))).toBe(true);
    expect(isLifecycleEnvelope(fixture('sse-event.json'))).toBe(true);
  });

	it('rejects a configuration missing the UDP contract', () => {
		const configuration = fixture('config.json') as { effective: Record<string, unknown> };
		delete configuration.effective.udp;
		expect(isConfigurationView(configuration)).toBe(false);
	});

	it('rejects a configuration missing the active file path', () => {
		const configuration = { ...(fixture('config.json') as Record<string, unknown>) };
		delete configuration.config_path;
		expect(isConfigurationView(configuration)).toBe(false);
	});

  it('rejects schema drift and unknown lifecycle names', () => {
    expect(isLifecycleEnvelope({ ...(fixture('sse-event.json') as object), schema_version: 2 })).toBe(false);
    expect(isLifecycleEnvelope({ ...(fixture('sse-event.json') as object), type: 'exchange.unknown' })).toBe(false);
  });
});
