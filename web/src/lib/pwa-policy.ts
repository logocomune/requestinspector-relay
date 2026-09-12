export interface RequestTarget { method: string; url: string | URL }

export function isNetworkOnly(target: RequestTarget): boolean {
  if (target.method.toUpperCase() !== 'GET') return true;
  const url = typeof target.url === 'string' ? new URL(target.url, 'https://reqrelay.invalid') : target.url;
  return url.pathname === '/api' || url.pathname.startsWith('/api/');
}

export function isNavigationRequest(request: Request): boolean {
  return request.method === 'GET' && request.mode === 'navigate';
}
