export interface UserAgentDetails { browser: string; engine: string; operatingSystem: string; device: string }

export function userAgentParserURL(value: string): string {
  return `https://useragents.io/parse/external?ua=${encodeURIComponent(value)}`;
}

export function inspectUserAgent(value: string): UserAgentDetails {
  return {
    browser: first(value, [[/Edg\//, 'Edge'], [/Firefox\//, 'Firefox'], [/(?:Chrome|CriOS)\//, 'Chrome'], [/Safari\//, 'Safari']]),
    engine: first(value, [[/Gecko\//, 'Gecko'], [/(?:AppleWebKit|Chrome|Safari)\//, 'WebKit/Blink']]),
    operatingSystem: first(value, [[/Windows NT/, 'Windows'], [/(?:iPhone|iPad)/, 'iOS'], [/Android/, 'Android'], [/Mac OS X/, 'macOS'], [/Linux/, 'Linux']]),
    device: first(value, [[/(?:Mobile|iPhone|Android)/, 'Mobile'], [/(?:iPad|Tablet)/, 'Tablet']], 'Desktop')
  };
}

function first(value: string, candidates: Array<[RegExp, string]>, fallback = 'Unknown'): string {
  return candidates.find(([pattern]) => pattern.test(value))?.[1] ?? fallback;
}
