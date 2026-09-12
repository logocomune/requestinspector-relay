const decoder = new TextDecoder('utf-8', { fatal: true });

export interface PayloadPresentation {
  kind: 'json' | 'text' | 'binary';
  text: string;
  jsonError?: string;
}

export function presentPayload(bytes: Uint8Array, contentType: string): PayloadPresentation {
  let text: string;
  try {
    text = decoder.decode(bytes);
  } catch {
    return { kind: 'binary', text: hexDump(bytes) };
  }
  if (contentType.toLowerCase().includes('json')) {
    try {
      return { kind: 'json', text: JSON.stringify(JSON.parse(text), null, 2) };
    } catch {
      return { kind: 'text', text, jsonError: 'Payload is not valid JSON.' };
    }
  }
  if (hasUnsafeControl(text)) return { kind: 'binary', text: hexDump(bytes) };
  return { kind: 'text', text };
}

export function hexDump(bytes: Uint8Array): string {
  const lines: string[] = [];
  for (let offset = 0; offset < bytes.length; offset += 16) {
    const chunk = bytes.slice(offset, offset + 16);
    const hex = [...chunk].map((byte) => byte.toString(16).padStart(2, '0')).join(' ').padEnd(47);
    const ascii = [...chunk].map((byte) => byte >= 32 && byte <= 126 ? String.fromCharCode(byte) : '.').join('');
    lines.push(`${offset.toString(16).padStart(8, '0')}  ${hex}  |${ascii}|`);
  }
  return lines.join('\n');
}

export function parseTotalBytes(contentRange: string | null, contentLength: string | null): number {
  const match = contentRange?.match(/^bytes \d+-\d+\/(\d+)$/);
  if (match) return Number(match[1]);
  return Number(contentLength ?? 0);
}

function hasUnsafeControl(value: string): boolean {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code < 32 && character !== '\n' && character !== '\r' && character !== '\t') return true;
  }
  return false;
}
