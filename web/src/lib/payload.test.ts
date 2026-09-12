import fc from 'fast-check';
import { describe, expect, it } from 'vitest';
import { hexDump, parseTotalBytes, presentPayload } from './payload';

describe('payload presentation', () => {
  it('formats JSON and falls back for invalid JSON and binary bytes', () => {
    expect(presentPayload(new TextEncoder().encode('{"ok":true}'), 'application/json')).toMatchObject({ kind: 'json', text: '{\n  "ok": true\n}' });
    expect(presentPayload(new TextEncoder().encode('{'), 'application/json')).toMatchObject({ kind: 'text', jsonError: 'Payload is not valid JSON.' });
    expect(presentPayload(Uint8Array.of(0xff, 0), 'application/octet-stream').kind).toBe('binary');
  });

  it('hex output represents every input byte exactly once', () => {
    fc.assert(fc.property(fc.uint8Array({ maxLength: 1024 }), (bytes) => {
      const pairs = hexDump(bytes).split('\n').flatMap((line) => line.slice(10, 57).trim().split(/\s+/).filter(Boolean));
      expect(pairs).toEqual([...bytes].map((byte) => byte.toString(16).padStart(2, '0')));
    }), { seed: 9072026, numRuns: 300 });
  });

  it('formats hex dump with offset, hex bytes, and visible ASCII column', () => {
    const input = new Uint8Array([
      0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x20, 0x57, 0x6f, 0x72, 0x6c, 0x64, 0x21, 0x00, 0x1f, 0x7f, 0x80
    ]);
    expect(hexDump(input)).toBe('00000000  48 65 6c 6c 6f 20 57 6f 72 6c 64 21 00 1f 7f 80  |Hello World!....|');
  });

  it('aligns visible ASCII column for partial rows', () => {
    const input = new TextEncoder().encode('ABC');
    expect(hexDump(input)).toBe('00000000  41 42 43                                         |ABC|');
    expect(hexDump(new Uint8Array())).toBe('');
  });

  it('parses partial and complete response sizes', () => {
    expect(parseTotalBytes('bytes 0-9/25', '10')).toBe(25);
    expect(parseTotalBytes(null, '7')).toBe(7);
  });
});
