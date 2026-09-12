export type TransportFilter = 'all' | 'http' | 'udp';

export function exchangeTransport(exchange: { transport?: 'http' | 'udp' }): 'http' | 'udp' {
  return exchange.transport === 'udp' ? 'udp' : 'http';
}

export function filterByTransport<T extends { transport?: 'http' | 'udp' }>(items: T[], filter: TransportFilter): T[] {
  if (filter === 'all') return items;
  return items.filter((item) => exchangeTransport(item) === filter);
}
