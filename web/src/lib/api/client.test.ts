import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIError, api } from './client';

afterEach(() => vi.unstubAllGlobals());

describe('exchange deletion', () => {
  it('deletes encoded exchange IDs from persistent history', async () => {
    const request = vi.fn().mockResolvedValue(new Response(JSON.stringify({ deleted: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }));
    vi.stubGlobal('fetch', request);

    await expect(api.deleteExchange('id/one')).resolves.toEqual({ deleted: true });
    expect(request).toHaveBeenCalledWith('/api/v1/exchanges/id%2Fone', expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }));
  });

  it('preserves structured deletion errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'exchange_active', message: 'Active exchange cannot be deleted.' }
    }), { status: 409, headers: { 'Content-Type': 'application/json' } })));

    await expect(api.deleteExchange('active')).rejects.toEqual(expect.objectContaining<Partial<APIError>>({ status: 409, code: 'exchange_active' }));
  });

  it('clears all history exchanges', async () => {
    const request = vi.fn().mockResolvedValue(new Response(JSON.stringify({ removed: 3 }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }));
    vi.stubGlobal('fetch', request);

    await expect(api.clearExchanges()).resolves.toEqual({ removed: 3 });
    expect(request).toHaveBeenCalledWith('/api/v1/exchanges', expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }));
  });
});
