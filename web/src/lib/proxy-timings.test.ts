import { describe, expect, it } from 'vitest';
import { formatDurationMilliseconds } from './proxy-timings';

describe('formatDurationMilliseconds', () => {
  it('converts microseconds to milliseconds with three decimals', () => {
    expect(formatDurationMilliseconds(152865)).toBe('152.865 ms');
    expect(formatDurationMilliseconds(149)).toBe('0.149 ms');
  });

  it('shows a placeholder for an unreached phase', () => {
    expect(formatDurationMilliseconds(undefined)).toBe('—');
  });
});
