import { describe, expect, it } from 'vitest';
import { inspectUserAgent, userAgentParserURL } from './user-agent';

describe('user agent parser URL', () => {
  it('encodes the raw user agent and targets the external parser', () => {
    expect(userAgentParserURL('Mozilla/5.0 (Test; x=y)')).toBe(
      'https://useragents.io/parse/external?ua=Mozilla%2F5.0%20(Test%3B%20x%3Dy)'
    );
  });
});

describe('user agent inspection', () => {
  it('keeps explicit unknown fallback', () => {
    expect(inspectUserAgent('custom-agent')).toEqual({ browser: 'Unknown', engine: 'Unknown', operatingSystem: 'Unknown', device: 'Desktop' });
  });

  it('classifies common browser fields without hiding raw value', () => {
    expect(inspectUserAgent('Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120.0 Mobile')).toEqual({
      browser: 'Chrome', engine: 'WebKit/Blink', operatingSystem: 'Android', device: 'Mobile'
    });
  });
});
