import { expect, test } from '@playwright/test';

test('publishes install metadata and controls root scope', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle('RequestInspector Relay');
  const manifest = await page.locator('link[rel="manifest"]').getAttribute('href');
  expect(manifest).toBe('/manifest.webmanifest');
  const metadata = await page.request.get('/manifest.webmanifest').then((response) => response.json());
  expect(metadata).toMatchObject({ id: '/', name: 'RequestInspector Relay', start_url: '/', scope: '/', display: 'standalone' });
  expect(metadata.icons).toHaveLength(4);
  await page.evaluate(() => navigator.serviceWorker.ready);
  expect(await page.evaluate(() => navigator.serviceWorker.controller?.scriptURL.endsWith('/service-worker.js'))).toBe(true);
  await page.goto('/about');
  await expect(page.getByRole('status')).toContainText(/Live|Offline/);
});

test('reloads shell offline without caching API responses', async ({ page, context }) => {
  await page.goto('/');
  await page.evaluate(() => navigator.serviceWorker.ready);
  await page.reload();
  const cachedAPI = await page.evaluate(async () => {
    const names = await caches.keys();
    const matches = await Promise.all(names.map(async (name) => (await caches.open(name)).match('/api/v1/status')));
    return matches.some(Boolean);
  });
  expect(cachedAPI).toBe(false);
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByText('Offline / live data unavailable')).toBeVisible();
  await expect(page.getByRole('link', { name: 'RequestInspector Relay' })).toBeVisible();
});
