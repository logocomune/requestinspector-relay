export interface UpdateController { available: boolean; apply: () => void }

export function serviceWorkerRegistrationOptions(development: boolean): RegistrationOptions {
  return { scope: '/', type: development ? 'module' : 'classic' };
}

export async function registerServiceWorker(onUpdate: (controller: UpdateController) => void): Promise<void> {
  if (!('serviceWorker' in navigator)) return;
  const registration = await navigator.serviceWorker.register(
    '/service-worker.js',
    serviceWorkerRegistrationOptions(import.meta.env.DEV)
  );
  let reloading = false;
  let applyRequested = false;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (!applyRequested || reloading) return;
    reloading = true;
    location.reload();
  });
  const updateController = (worker: ServiceWorker): UpdateController => ({
    available: true,
    apply: () => {
      applyRequested = true;
      worker.postMessage({ type: 'SKIP_WAITING' });
    }
  });
  const inspect = (worker: ServiceWorker | null) => {
    if (!worker) return;
    worker.addEventListener('statechange', () => {
      if (worker.state === 'installed' && navigator.serviceWorker.controller) {
        onUpdate(updateController(worker));
      }
    });
  };
  if (registration.waiting) onUpdate(updateController(registration.waiting));
  registration.addEventListener('updatefound', () => inspect(registration.installing));
}
