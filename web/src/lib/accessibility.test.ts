import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const css = readFileSync(new URL('../app.css', import.meta.url), 'utf8');

function color(theme: 'light' | 'dark', token: string): string {
  const selector = theme === 'light' ? ":root[data-theme='light']" : ":root[data-theme='dark']";
  const block = css.slice(css.indexOf(selector), css.indexOf('}', css.indexOf(selector)));
  const value = block.match(new RegExp(`--rl-${token}:\\s*(#[0-9a-fA-F]{6})`))?.[1];
  if (!value) throw new Error(`Missing ${theme} ${token} token`);
  return value;
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((start) => Number.parseInt(hex.slice(start, start + 2), 16) / 255)
    .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrast(first: string, second: string): number {
  const values = [luminance(first), luminance(second)].sort((a, b) => b - a);
  return (values[0] + 0.05) / (values[1] + 0.05);
}

describe('accessibility foundations', () => {
  it.each(['light', 'dark'] as const)('%s semantic text colors meet WCAG AA on page background', (theme) => {
    for (const token of ['text', 'text-muted', 'link', 'error']) expect(contrast(color(theme, token), color(theme, 'page')), token).toBeGreaterThanOrEqual(4.5);
  });

  it('provides visible keyboard focus and reduced-motion behavior', () => {
    expect(css).toContain(':focus-visible');
    expect(css).toContain('@media (prefers-reduced-motion: reduce)');
  });
});
