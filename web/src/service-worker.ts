/// <reference lib="webworker" />

import { build, files, prerendered, version } from '$service-worker';
import { serviceWorkerShellAssets } from '$lib/pwa-assets';
import { isNavigationRequest, isNetworkOnly } from '$lib/pwa-policy';

declare const self: ServiceWorkerGlobalScope;

const cacheName = `reqrelay-shell-${version}`;
const shellAssets = serviceWorkerShellAssets(build, files, prerendered, import.meta.env.DEV);

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    await (await caches.open(cacheName)).addAll(shellAssets);
    if (!self.registration.active) await self.skipWaiting();
  })());
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter((name) => name.startsWith('reqrelay-shell-') && name !== cacheName).map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener('message', (event) => {
  if (event.data?.type === 'SKIP_WAITING') void self.skipWaiting();
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (isNetworkOnly(request) || new URL(request.url).origin !== self.location.origin) return;
  if (isNavigationRequest(request)) {
    event.respondWith(fetch(request).catch(async () => {
      const cached = await caches.match('/');
      if (!cached) throw new Error('Offline shell unavailable.');
      return cached;
    }));
    return;
  }
  if (request.method === 'GET') {
    event.respondWith(caches.match(request).then((cached) => cached ?? fetch(request)));
  }
});
