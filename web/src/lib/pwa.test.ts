import { afterEach, describe, expect, it, vi } from 'vitest';
import config from '../../svelte.config.js';
import { registerServiceWorker, serviceWorkerRegistrationOptions } from './pwa';
import { serviceWorkerShellAssets } from './pwa-assets';

describe('service-worker registration', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('uses module workers during development and classic workers in production', () => {
    expect(serviceWorkerRegistrationOptions(true)).toEqual({ scope: '/', type: 'module' });
    expect(serviceWorkerRegistrationOptions(false)).toEqual({ scope: '/', type: 'classic' });
  });

  it('registers one development-compatible worker', async () => {
    const register = vi.fn().mockResolvedValue({ waiting: null, addEventListener: vi.fn() });
    vi.stubGlobal('navigator', {
      serviceWorker: { register, addEventListener: vi.fn(), controller: null }
    });

    await registerServiceWorker(vi.fn());

    expect(register).toHaveBeenCalledOnce();
    expect(register).toHaveBeenCalledWith('/service-worker.js', { scope: '/', type: 'module' });
  });

  it('disables SvelteKit automatic registration', () => {
    expect(config.kit?.serviceWorker?.register).toBe(false);
  });

  it('excludes the production fallback from Vite development precache', () => {
    expect(serviceWorkerShellAssets([], [], [], true)).toEqual(['/']);
    expect(serviceWorkerShellAssets([], [], [], false)).toEqual(['/', '/index.html']);
  });
});
