import { describe, expect, it } from 'vitest';
import { readTheme, resolveTheme, themeStorageKey, validTheme } from './theme';

describe('theme', () => {
  it.each([
    ['system', false, 'light'], ['system', true, 'dark'], ['light', true, 'light'], ['dark', false, 'dark']
  ] as const)('resolves %s with systemDark=%s', (choice, systemDark, expected) => {
    expect(resolveTheme(choice, systemDark)).toBe(expected);
  });

  it('accepts only persisted theme values', () => {
    expect(['system', 'light', 'dark'].every(validTheme)).toBe(true);
    expect(validTheme('sepia')).toBe(false);
    expect(readTheme({ getItem: (key) => key === themeStorageKey ? 'invalid' : null })).toBe('system');
  });
});
