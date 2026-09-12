import { expect, test } from '@playwright/test';
import dgram from 'node:dgram';
import net from 'node:net';

function udpSend(port: number, payload: Buffer): Promise<void> {
  return new Promise((resolve, reject) => {
    const socket = dgram.createSocket('udp4');
    socket.send(payload, port, '127.0.0.1', (error) => { socket.close(); error ? reject(error) : resolve(); });
  });
}

async function startUDPUpstream(): Promise<{ port: number; close: () => Promise<void> }> {
  const socket = dgram.createSocket('udp4');
  socket.on('message', (message, peer) => { socket.send(Buffer.concat([Buffer.from('proxy:'), message]), peer.port, peer.address); });
  await new Promise<void>((resolve, reject) => {
    socket.once('error', reject);
    socket.bind(0, '127.0.0.1', resolve);
  });
  const address = socket.address();
  if (typeof address === 'string') throw new Error('UDP upstream address unavailable.');
  return { port: address.port, close: () => new Promise((resolve) => socket.close(resolve)) };
}

async function occupyTCPPort(): Promise<{ port: number; close: () => Promise<void> }> {
  const server = net.createServer();
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('TCP listener address unavailable.');
  return { port: address.port, close: () => new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve())) };
}

async function occupyUDPPort(): Promise<{ port: number; close: () => Promise<void> }> {
  const socket = dgram.createSocket('udp4');
  await new Promise<void>((resolve, reject) => {
    socket.once('error', reject);
    socket.bind(0, '127.0.0.1', resolve);
  });
  const address = socket.address();
  if (typeof address === 'string') throw new Error('UDP listener address unavailable.');
  return { port: address.port, close: () => new Promise((resolve) => socket.close(resolve)) };
}

test('inspects capture and proxy exchanges across live and history views', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Realtime' })).toBeVisible();
  await expect(page.locator('fieldset[aria-label="Color theme"]')).toHaveCount(0);
  await page.locator('summary[aria-label="Clear requests"]').click();
  await expect(page.getByRole('button', { name: 'Clear Realtime', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Clear Realtime and History' })).toBeDisabled();
  await page.getByRole('heading', { name: 'Realtime' }).click();
  await expect(page.locator('summary[aria-label="Clear requests"]').locator('..')).not.toHaveAttribute('open', '');
  await expect(page.locator('dl[aria-label="Traffic metrics"]')).toHaveCount(0);

  const capture = await page.request.post('http://127.0.0.1:4174/capture?case=browser', {
    data: '{"hello":"world"}',
    headers: { 'Content-Type': 'application/json', 'User-Agent': 'ReqRelay-E2E/1.0', 'X-Trace': 'capture' }
  });
  expect(capture.status()).toBe(200);
  await expect(page.getByText('/capture?case=browser HTTP/1.1')).toBeVisible();
  await expect(page.getByRole('button', { name: 'JSON', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open User-Agent parser' })).toHaveAttribute('href', /useragents\.io\/parse\/external/);
  await expect(page.getByRole('heading', { name: 'Response' })).toHaveCount(0);

  expect((await page.request.get('http://127.0.0.1:4174/newer')).status()).toBe(200);
  await expect(page.getByText('/capture?case=browser HTTP/1.1')).toBeVisible();
  await expect(page.getByText('/newer HTTP/1.1')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Request inspector' })).toHaveCount(2);
  const emptyInspector = page.getByRole('region', { name: 'Request inspector' }).first();
  await expect(emptyInspector).toContainText('/newer HTTP/1.1');
  await expect(emptyInspector.locator('section[aria-label="request payload"]')).toHaveCount(0);
  const captureInspector = page.getByRole('region', { name: 'Request inspector' }).filter({ hasText: '/capture?case=browser HTTP/1.1' });
  await expect(captureInspector.locator('section[aria-label="request payload"]')).toBeVisible();

  await emptyInspector.getByRole('button', { name: /Remove .* from Realtime/ }).click();
  await expect(page.getByText('Request removed from Realtime.', { exact: false })).toBeVisible();
  await expect(page.getByText('/newer HTTP/1.1')).toHaveCount(0);
  await page.getByRole('tab', { name: /History/ }).click();
  const selectedHistoryRow = page.getByRole('option').filter({ hasText: '/newer' });
  await expect(selectedHistoryRow).toBeVisible();
  await expect(selectedHistoryRow.getByText('HTTP', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: /Realtime/ }).click();

  page.once('dialog', (dialog) => dialog.accept());
  await captureInspector.getByRole('button', { name: /Delete .* from Realtime and History/ }).click();
  await expect(page.getByText('Request deleted from Realtime and History.', { exact: false })).toBeVisible();
  await expect(page.getByText('/capture?case=browser HTTP/1.1')).toHaveCount(0);
  await page.getByRole('tab', { name: /History/ }).click();
  await expect(page.getByRole('option').filter({ hasText: '/capture' })).toHaveCount(0);
  await page.getByRole('tab', { name: /Realtime/ }).click();

  const binary = await page.request.post('http://127.0.0.1:4174/binary', {
    data: Buffer.alloc(70_000, 0xff), headers: { 'Content-Type': 'application/octet-stream' }
  });
  expect(binary.status()).toBe(200);
  const binaryInspector = page.getByRole('region', { name: 'Request inspector' }).first();
  await expect(binaryInspector).toContainText('/binary HTTP/1.1');
  await expect(binaryInspector.getByRole('button', { name: 'Hex' })).toBeVisible();
  await expect(binaryInspector.getByText('Preview truncated. Load full body below.')).toBeVisible();
  await binaryInspector.getByRole('button', { name: 'Load next 64 KiB' }).click();
  await expect(binaryInspector.getByText('Full body loaded')).toBeVisible();
  await expect(binaryInspector.getByRole('link', { name: 'Download' })).toBeVisible();

  const oversized = await page.request.post('http://127.0.0.1:4174/oversized', { data: Buffer.alloc(1_048_577) });
  expect(oversized.status()).toBe(413);

  await page.locator('summary', { hasText: 'HTTP:' }).click();
  await page.getByRole('button', { name: 'Proxy', exact: true }).click();
  await expect(page.locator('summary', { hasText: 'HTTP:' })).toContainText(/HTTP:\s*proxy/i);
  const proxy = await page.request.get('http://127.0.0.1:4174/fixture.json');
  expect(proxy.status()).toBe(200);
  expect(await proxy.text()).toContain('proxied');
  await page.getByRole('tab', { name: /Realtime/ }).click();
  const proxyInspector = page.getByRole('region', { name: 'Request inspector' }).first();
  await expect(proxyInspector).toContainText('/fixture.json HTTP/1.1');
  await expect(proxyInspector.getByRole('heading', { name: 'Response' })).toBeVisible();
  await expect(proxyInspector.getByText('Proxy timings')).toBeVisible();

  const emptyProxy = await page.request.head('http://127.0.0.1:4174/fixture.json');
  expect(emptyProxy.status()).toBe(200);
  const emptyProxyInspector = page.getByRole('region', { name: 'Request inspector' }).first();
  await expect(emptyProxyInspector.getByText('HEAD', { exact: true })).toBeVisible();
  await expect(emptyProxyInspector.getByText('/fixture.json HTTP/1.1', { exact: true })).toBeVisible();
  await expect(emptyProxyInspector.locator('section[aria-label="request payload"]')).toHaveCount(0);
  await expect(emptyProxyInspector.locator('section[aria-label="response payload"]')).toHaveCount(0);
});

test('keeps tab navigation and narrow layout accessible', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/?tab=history');
  await expect(page.getByRole('tab', { name: /History/ })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('tab', { name: /History/ }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Info' })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('tab', { name: /Realtime/ }).click();
  const realtimeFilter = page.locator('fieldset[aria-label="Realtime transport filter"]');
  await expect(realtimeFilter).toBeVisible();
  await realtimeFilter.locator('button').nth(2).click();
  await expect(realtimeFilter.locator('button').nth(2)).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('tab', { name: /History/ }).click();
  const historyFilter = page.locator('fieldset[aria-label="History transport filter"]');
  await expect(historyFilter).toBeVisible();
  await historyFilter.locator('button').nth(2).click();
  await expect(historyFilter.locator('button').nth(2)).toHaveAttribute('aria-pressed', 'true');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await expect(page.locator('button:not([disabled])').first()).toBeVisible();
  await page.locator('summary[aria-label="More pages"]').click();
  await expect(page.locator('details').getByRole('link', { name: 'About' })).toBeVisible();
});

test('inspects capture, proxy, and discarded UDP datagrams', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('status', { name: 'Live' })).toBeVisible();
  await udpSend(4176, Buffer.from('{"kind":"capture"}'));
  await expect.poll(async () => page.request.get('/api/v1/exchanges').then((response) => response.json()).then((value) => value.items.some((item: { transport?: string }) => item.transport === 'udp'))).toBe(true);
  await page.goto('/?tab=history');
  await expect(page.getByRole('heading', { name: 'Datagram' })).toBeVisible();
  const captureInspector = page.getByRole('region', { name: 'Request inspector' }).first();
  await expect(captureInspector).toContainText('127.0.0.1');
  await expect(captureInspector.getByRole('button', { name: 'Text' })).toBeVisible();
  await expect(captureInspector.getByRole('button', { name: 'JSON' })).toBeVisible();
  await expect(captureInspector.getByRole('button', { name: 'Hex' })).toBeVisible();
  await expect(captureInspector.getByRole('button', { name: 'Raw' })).toBeVisible();
  await expect(captureInspector.getByRole('link', { name: 'Download' })).toHaveAttribute('href', /\/datagram\/body$/);

  const upstream = await startUDPUpstream();
  try {
    let configuration = await page.request.get('/api/v1/config').then((response) => response.json());
    const proxyUpdate = await page.request.put('/api/v1/config', { data: {
      expected_revision: configuration.revision,
      overrides: { 'udp.mode': 'proxy', 'udp.upstream': `127.0.0.1:${upstream.port}`, 'udp.capture_response': '' }
    } });
    expect(proxyUpdate.ok()).toBe(true);
    await udpSend(4176, Buffer.from('forward-me'));
    await expect.poll(async () => page.request.get('/api/v1/exchanges').then((response) => response.json()).then((value) => value.items.some((item: { transport?: string; mode?: string }) => item.transport === 'udp' && item.mode === 'proxy'))).toBe(true);
    await page.goto('/?tab=history');
    await expect(page.getByText('Proxy datagram timeline')).toBeVisible();

    configuration = await page.request.get('/api/v1/config').then((response) => response.json());
    const limitUpdate = await page.request.put('/api/v1/config', { data: {
      expected_revision: configuration.revision, overrides: { 'udp.max_datagram_bytes': 4 }
    } });
    expect(limitUpdate.ok()).toBe(true);
    await udpSend(4176, Buffer.from('oversized'));
    await page.goto('/?tab=history');
    await expect(page.getByText('Configured UDP datagram limit rejected this payload.')).toBeVisible();
  } finally {
    await upstream.close();
  }
});

test('persists settings with revisions and runs confirmed SQLite maintenance', async ({ page }) => {
  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await expect(page.locator('fieldset[aria-label="Color theme"]')).toBeVisible();
  const httpTab = page.getByRole('tab', { name: 'HTTP', exact: true });
  await expect(httpTab).toHaveAttribute('aria-selected', 'true');
  await expect(httpTab).toHaveClass(/bg-action\/10/);
  await expect(page.getByRole('heading', { name: 'HTTP Capture' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'HTTP Proxy' })).toBeVisible();
  await page.getByRole('tab', { name: 'Storage' }).click();
  await expect(page.getByRole('tabpanel', { name: 'Storage' })).toBeVisible();

  const ramCapacity = page.getByLabel('RAM exchanges');
  const settingsSavedToast = page.getByRole('status').filter({ hasText: 'Settings saved.' });
  await ramCapacity.fill('101');
  await page.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByText('Saved configuration revision', { exact: false })).toBeVisible();
  await expect(settingsSavedToast.first()).toBeVisible();
  const ramCapacityField = ramCapacity.locator('..').locator('..');
  await expect(ramCapacityField).toContainText('UI override');
  await ramCapacityField.getByRole('button', { name: 'Reset setting' }).click();
  await expect(ramCapacityField).toContainText('Will reset');
  await page.getByRole('button', { name: 'Save settings' }).click();
  await expect(settingsSavedToast.first()).toBeVisible();
  await expect(ramCapacity).toHaveValue('100');
  await expect(page.getByRole('button', { name: 'Save settings' })).toBeDisabled();

  await ramCapacity.fill('0');
  await expect(ramCapacity).toHaveValue('0');
  await expect(page.getByRole('button', { name: 'Save settings' })).toBeEnabled();
  await page.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('alert')).toContainText('history capacity must be positive');

  await ramCapacity.fill('101');
  await page.evaluate(async () => {
    const configuration = await fetch('/api/v1/config').then((response) => response.json());
    await fetch('/api/v1/config', {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ expected_revision: configuration.revision, overrides: { 'preview.bytes': 64 } })
    });
  });
  await page.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('alert')).toContainText('changed elsewhere');
  const previewBytes = page.getByLabel('Preview bytes');
  await expect(previewBytes).toHaveValue('64');
  await previewBytes.locator('..').locator('..').getByRole('button', { name: 'Reset setting' }).click();

  await expect(page.getByRole('heading', { name: 'SQLite storage and maintenance' })).toBeVisible();
  await page.getByLabel('Keep days').fill('30');
  await page.getByRole('button', { name: 'Preview cleanup' }).click();
  await expect(page.getByText(/Cleanup preview:/)).toBeVisible();
  await page.getByRole('button', { name: 'Run compaction' }).click();
  await expect(page.getByText(/Compaction completed:/).first()).toBeVisible();
  await page.getByRole('checkbox', { name: 'I understand operational impact' }).check();
  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', { name: 'Run full VACUUM' }).click();
  await expect(page.getByText('Full VACUUM completed and database integrity verified.').first()).toBeVisible();
});

test('keeps management active and shows a toast when listener reload fails', async ({ page }) => {
  const occupied = await occupyTCPPort();
  try {
    await page.goto('/settings');
    const before = await page.request.get('/api/v1/config').then((response) => response.json());
    await page.getByRole('tab', { name: 'Administration' }).click();
    const unavailableListener = `127.0.0.1:${occupied.port}`;
    await page.getByLabel('Management listener').fill(unavailableListener);
    await page.getByRole('button', { name: 'Save settings' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Management listener is unavailable; current endpoint remains active.' })).toBeVisible();
    await expect(page.getByLabel('Management listener')).toHaveValue(unavailableListener);
    const health = await page.request.get('/api/v1/health');
    expect(health.ok()).toBe(true);
    const after = await page.request.get('/api/v1/config').then((response) => response.json());
    expect(after.revision).toBe(before.revision);
    expect(after.effective.listeners.management).toBe(before.effective.listeners.management);
  } finally {
    await occupied.close();
  }
});

test('reloads HTTP ingest listener and reports the active address', async ({ page }) => {
  const reserved = await occupyTCPPort();
  const nextListener = `127.0.0.1:${reserved.port}`;
  await reserved.close();
  await page.goto('/settings');
  const before = await page.request.get('/api/v1/config').then((response) => response.json());
  await page.getByLabel('Traffic listener').fill(nextListener);
  await page.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status').filter({ hasText: `HTTP ingest reloaded on ${nextListener}.` })).toBeVisible();
  const traffic = await page.request.get(`http://${nextListener}/after-traffic-reload`);
  expect(traffic.status()).toBeGreaterThanOrEqual(200);
  const status = await page.request.get('/api/v1/status').then((response) => response.json());
  expect(status.traffic_listener).toBe(nextListener);

  const current = await page.request.get('/api/v1/config').then((response) => response.json());
  const reset = await page.request.delete(`/api/v1/config/overrides/listeners.traffic?expected_revision=${current.revision}`);
  expect(reset.ok()).toBe(true);
  await expect.poll(async () => page.request.get('/api/v1/status').then((response) => response.json()).then((value) => value.traffic_listener)).toBe(before.effective.listeners.traffic);
});

test('keeps HTTP ingest active and shows a toast when traffic listener reload fails', async ({ page }) => {
  const occupied = await occupyTCPPort();
  try {
    await page.goto('/settings');
    const before = await page.request.get('/api/v1/config').then((response) => response.json());
    const unavailableListener = `127.0.0.1:${occupied.port}`;
    await page.getByLabel('Traffic listener').fill(unavailableListener);
    await page.getByRole('button', { name: 'Save settings' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Traffic listener is unavailable; current endpoint remains active.' })).toBeVisible();
    await expect(page.getByLabel('Traffic listener')).toHaveValue(unavailableListener);
    const traffic = await page.request.get(`http://${before.effective.listeners.traffic}/after-rejected-reload`);
    expect(traffic.status()).toBeGreaterThanOrEqual(200);
    const after = await page.request.get('/api/v1/config').then((response) => response.json());
    expect(after.revision).toBe(before.revision);
    expect(after.effective.listeners.traffic).toBe(before.effective.listeners.traffic);
  } finally {
    await occupied.close();
  }
});

test('keeps UDP settings and transport mode independent from HTTP', async ({ page }) => {
  await page.goto('/settings');
	const originalHTTPMode = await page.request.get('/api/v1/config').then((response) => response.json()).then((value) => value.effective.mode);
	await page.getByRole('tab', { name: 'UDP' }).click();
  const udpEnabled = page.getByLabel('Enabled');
  await udpEnabled.selectOption('true');
	await page.locator('#setting-udp\\.mode').selectOption('proxy');
	await expect(page.getByRole('heading', { name: 'UDP Proxy' })).toBeVisible();
	await page.locator('#setting-udp\\.upstream').fill('127.0.0.1:4175');
  await page.getByRole('button', { name: 'Save settings' }).click();
	await expect(page.getByText(/UDP ingest reloaded on /)).toBeVisible();
	await expect(page.getByRole('heading', { name: 'UDP Capture' })).toBeVisible();
	await expect(page.getByLabel('Enabled').locator('..').locator('..')).not.toContainText('Restart required');
	await expect.poll(async () => page.request.get('/api/v1/status').then((response) => response.json()).then((value) => value.udp.enabled)).toBe(true);

  await page.goto('/');
  const primaryTransport = page.getByRole('navigation', { name: 'Primary' }).locator('summary', { hasText: 'UDP:' });
  await primaryTransport.click();
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'Primary' }).locator('summary', { hasText: 'UDP:' })).toContainText(/UDP:\s*proxy/i);
  await expect.poll(async () => page.request.get('/api/v1/config').then((response) => response.json()).then((value) => value.effective.udp.mode)).toBe('proxy');
	await expect.poll(async () => page.request.get('/api/v1/config').then((response) => response.json()).then((value) => value.effective.mode)).toBe(originalHTTPMode);
});

test('keeps UDP ingest active and shows a toast when UDP listener reload fails', async ({ page }) => {
	const occupied = await occupyUDPPort();
	try {
		await page.goto('/settings');
		const before = await page.request.get('/api/v1/config').then((response) => response.json());
		const beforeStatus = await page.request.get('/api/v1/status').then((response) => response.json());
		await page.getByRole('tab', { name: 'UDP' }).click();
		await page.getByLabel('Enabled').selectOption('true');
		const unavailableListener = `127.0.0.1:${occupied.port}`;
		await page.getByLabel('Listener').fill(unavailableListener);
		await page.getByRole('button', { name: 'Save settings' }).click();
		await expect(page.getByRole('status').filter({ hasText: 'UDP listener or upstream is unavailable; current endpoint remains active.' })).toBeVisible();
		await expect(page.getByLabel('Listener')).toHaveValue(unavailableListener);
		const after = await page.request.get('/api/v1/config').then((response) => response.json());
		expect(after.revision).toBe(before.revision);
		expect(after.effective.udp.listen).toBe(before.effective.udp.listen);
		const status = await page.request.get('/api/v1/status').then((response) => response.json());
		expect(status.udp.listener).toBe(beforeStatus.udp.listener);
	} finally {
		await occupied.close();
	}
});

test('opens Help topics and explains capture and proxy flows', async ({ page }) => {
  await page.goto('/help');
  await expect(page.getByRole('heading', { name: 'How RequestInspector Relay works' })).toBeVisible();
  await expect(page.getByText('RequestInspector Relay receives HTTP and UDP traffic', { exact: false })).toBeVisible();
  await page.locator('summary[aria-label="Help topics"]').click();
  await page.locator('details').filter({ has: page.getByRole('link', { name: 'UDP Proxy' }) }).getByRole('link', { name: 'UDP Proxy' }).click();
  await expect(page.getByRole('heading', { name: 'UDP Proxy' })).toBeVisible();
  await expect(page.getByText('UDP client A → RequestInspector Relay → fixed UDP upstream')).toBeVisible();
  await page.getByRole('link', { name: 'HTTP Capture' }).last().click();
  await expect(page.getByRole('heading', { name: 'HTTP Capture' })).toBeVisible();
  await expect(page.getByText('configured HTTP status, headers, and body')).toBeVisible();
});

test('serves privacy-safe information pages with zoom and reduced motion support', async ({ page }) => {
  const apiRequests: string[] = [];
  page.on('request', (request) => { if (request.url().includes('/api/v1/')) apiRequests.push(new URL(request.url()).pathname); });
  await page.route('**/api/v1/session', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify({ login_required: true, authenticated: false })
  }));
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/about');
  await expect(page).toHaveTitle('About - RequestInspector Relay');
  await expect(page.getByRole('heading', { name: 'About RequestInspector Relay' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'RequestInspector.com (Free Online)' })).toHaveAttribute('href', 'http://requestinspector.com');
  await expect(page.getByRole('link', { name: 'GitHub repository' })).toHaveAttribute('href', 'https://github.com/logocomune/requestinspector-relay');
  await expect(page.getByRole('link', { name: 'GitHub issues' })).toHaveAttribute('href', 'https://github.com/logocomune/requestinspector-relay/issues');
  await expect(page.getByRole('heading', { name: 'Disclaimer' })).toBeVisible();
  await expect(page.getByText('provided "as is"', { exact: false })).toBeVisible();
  await expect(page.getByText('Unknown')).toHaveCount(3);
  await expect.poll(() => apiRequests).toContain('/api/v1/session');
  expect(apiRequests).not.toContain('/api/v1/status');
  await expect(page.locator('body')).not.toContainText('127.0.0.1:4174');
  await expect(page.locator('body')).not.toContainText('/tmp/reqrelay-phase10-e2e.db');
  expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(true);

  await page.getByRole('navigation', { name: 'Project links' }).getByRole('link', { name: 'Credits' }).click();
  await expect(page).toHaveTitle('Credits - RequestInspector Relay');
  await expect(page.getByRole('heading', { name: 'Go backend libraries' })).toBeVisible();
  await expect(page.getByText('modernc SQLite')).toBeVisible();
  await expect(page.getByText('OpenLayers')).toHaveCount(0);

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.evaluate(() => { document.body.style.zoom = '2'; });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => getComputedStyle(document.activeElement as Element).outlineStyle)).not.toBe('none');
});
