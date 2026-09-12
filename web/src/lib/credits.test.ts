import { describe, expect, it } from 'vitest';
import { creditGroups } from './credits';

describe('dependency credits', () => {
  it('provides complete, unique, safely linked metadata', () => {
    const entries = creditGroups.flatMap((group) => group.entries);
    expect(entries[0].name).toBe('Go and standard library');
    expect(new Set(entries.map((entry) => entry.name)).size).toBe(entries.length);
    for (const entry of entries) {
      expect(entry.version).not.toBe('');
      expect(entry.license).toMatch(/^[A-Za-z0-9-.+]+$/);
      expect(new URL(entry.url).protocol).toBe('https:');
      expect(entry.purpose).not.toBe('');
    }
  });
});
