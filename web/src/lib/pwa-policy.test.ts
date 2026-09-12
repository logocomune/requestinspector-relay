import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { isNetworkOnly } from './pwa-policy';

describe('service-worker request policy', () => {
  it('keeps every API path, query, body endpoint, and SSE request network-only', () => {
    fc.assert(fc.property(
      fc.array(fc.stringMatching(/^[A-Za-z0-9_~-]{0,12}$/), { maxLength: 8 }),
      fc.string(),
      (segments, query) => {
        const path = `/api/${segments.join('/')}`;
        expect(isNetworkOnly({ method: 'GET', url: `https://host${path}?${encodeURIComponent(query)}` })).toBe(true);
      }
    ), { seed: 20260907, numRuns: 500 });
  });

  it('keeps every mutation network-only', () => {
    fc.assert(fc.property(fc.constantFrom('POST', 'PUT', 'PATCH', 'DELETE'), fc.webUrl(), (method, url) => {
      expect(isNetworkOnly({ method, url })).toBe(true);
    }), { seed: 20260908, numRuns: 300 });
  });

  it.each(['/api/v1/events', '/api/v1/session', '/api/v1/exchanges/id/request/body', '/api/v1/exchanges/id/datagram/body'])('classifies %s as network-only', (path) => {
    expect(isNetworkOnly({ method: 'GET', url: path })).toBe(true);
  });

  it('allows shell assets to use cache policy', () => {
    expect(isNetworkOnly({ method: 'GET', url: '/_app/immutable/app.js' })).toBe(false);
  });

  it('classifies canonical URL paths after dot-segment removal', () => {
    expect(isNetworkOnly({ method: 'GET', url: '/api/..' })).toBe(false);
  });
});
