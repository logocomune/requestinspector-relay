<script lang="ts">
  import { mdiContentCopy, mdiDownload } from '@mdi/js';
  import { untrack } from 'svelte';
  import { api } from '$lib/api/client';
  import { hexDump, parseTotalBytes, presentPayload } from '$lib/payload';
  import MdiIcon from './MdiIcon.svelte';
  interface Props {
    exchangeID: string; side: 'request' | 'response' | 'datagram'; available: boolean; contentType: string;
    encoding?: string; encodedBytes: number; decodedBytes?: number; truncated: boolean; previewError?: string; jsonHint?: boolean;
  }
  let { exchangeID, side, available, contentType, encoding, encodedBytes, decodedBytes, truncated, previewError, jsonHint = false }: Props = $props();
  let bytes = $state(new Uint8Array());
  let total = $state(0);
  let loading = $state(false);
  let error = $state('');
  let copied = $state('');
  let active = $state<'json' | 'text' | 'hex' | 'raw'>('text');
  let presentation = $derived(presentPayload(bytes, jsonHint ? 'application/json' : contentType));
  let shown = $derived(active === 'json' && presentation.kind === 'json' ? presentation.text : active === 'hex' ? hexDump(bytes) : active === 'raw' ? rawBytes(bytes) : new TextDecoder().decode(bytes));
  let complete = $derived(bytes.length >= total);

  $effect(() => {
    const identity = `${exchangeID}:${side}`;
    const initialTotal = encodedBytes;
    const canLoad = available;
    untrack(() => {
      identity;
      bytes = new Uint8Array(); total = initialTotal; error = ''; active = 'text';
      if (canLoad) void loadMore();
    });
  });

  async function loadMore(): Promise<void> {
    if (loading || (bytes.length > 0 && complete)) return;
    loading = true; error = '';
    try {
      const start = bytes.length;
      const response = await api.body(exchangeID, side, `bytes=${start}-${start + 65535}`);
      if (response.status === 416 && start === 0) { total = 0; return; }
      if (!response.ok) throw new Error(`Body load failed (${response.status}).`);
      const chunk = new Uint8Array(await response.arrayBuffer());
      if (chunk.length === 0) { total = bytes.length; return; }
      const merged = new Uint8Array(bytes.length + chunk.length);
      merged.set(bytes); merged.set(chunk, bytes.length); bytes = merged;
      total = parseTotalBytes(response.headers.get('Content-Range'), response.headers.get('Content-Length')) || bytes.length;
      if (presentation.kind === 'json') active = 'json';
      else if (presentation.kind === 'binary') active = 'hex';
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Body load failed.';
    } finally { loading = false; }
  }

  async function loadAll(): Promise<void> {
    while (bytes.length < total && !error) await loadMore();
  }

  async function copyAll(): Promise<void> {
    await loadAll();
    try {
      await navigator.clipboard.writeText(
        active === 'hex' || presentation.kind === 'binary'
          ? hexDump(bytes)
          : active === 'raw'
          ? rawBytes(bytes)
          : new TextDecoder().decode(bytes)
      );
      copied = 'Copied';
    } catch {
      copied = 'Copy failed';
    }
  }

  function rawBytes(value: Uint8Array): string {
    return [...value].map((byte) => byte >= 32 && byte <= 126 ? String.fromCharCode(byte) : `\\x${byte.toString(16).padStart(2, '0')}`).join('');
  }
</script>

<section class="mt-4" aria-label={`${side} payload`}>
  <div class="flex flex-wrap items-center gap-2 border-b border-border">
    {#if presentation.kind === 'json'}<button type="button" class="min-h-10 px-3" class:bg-surface-muted={active === 'json'} class:text-link={active !== 'json'} onclick={() => { active = 'json'; }}>JSON</button>{/if}
    <button type="button" class="min-h-10 px-3" class:bg-surface-muted={active === 'text'} class:text-link={active !== 'text'} onclick={() => { active = 'text'; }}>Text</button>
    <button type="button" class="min-h-10 px-3" class:bg-surface-muted={active === 'hex'} class:text-link={active !== 'hex'} onclick={() => { active = 'hex'; }}>Hex</button>
    <button type="button" class="min-h-10 px-3" class:bg-surface-muted={active === 'raw'} class:text-link={active !== 'raw'} onclick={() => { active = 'raw'; }}>Raw</button>
    <button type="button" class="ml-auto inline-flex min-h-10 items-center gap-1 rounded px-2 text-link" onclick={copyAll}><MdiIcon path={mdiContentCopy} size={18} /> Copy full</button>
    <a class="inline-flex min-h-10 items-center gap-1 rounded px-2 text-link" href={`/api/v1/exchanges/${encodeURIComponent(exchangeID)}/${side}/body`} download><MdiIcon path={mdiDownload} size={18} /> Download</a>
  </div>
  <div class="mt-2 flex flex-wrap gap-x-4 text-xs text-text-muted">
    <span>Type: {contentType || 'unknown'}</span>{#if encoding !== undefined}<span>Compression: {encoding || 'none'}</span>{/if}<span>Encoded: {encodedBytes} B</span>
    {#if decodedBytes !== undefined}<span>Decoded preview: {decodedBytes} B</span>{/if}<span>Loaded: {bytes.length}/{total} B</span>
  </div>
  {#if truncated}<p class="mt-2 text-sm text-kpi-text">Preview truncated. Load full body below.</p>{/if}
  {#if previewError}<p class="mt-2 text-sm text-error">{previewError}</p>{/if}
  {#if presentation.jsonError}<p class="mt-2 text-sm text-text-muted">{presentation.jsonError}</p>{/if}
  {#if error}<p class="mt-2 text-sm text-error" role="alert">{error} <button type="button" class="underline" onclick={loadMore}>Retry</button></p>{/if}
  <pre class="mt-2 max-h-80 min-h-20 overflow-auto rounded border border-border bg-page p-3 font-mono text-sm whitespace-pre">{shown}</pre>
  <div class="mt-2 flex items-center gap-3 text-sm" role="status">
    {#if loading}<span>Loading payload…</span>{:else if !complete}<button type="button" class="min-h-10 rounded border border-control px-3 text-link" onclick={loadMore}>Load next 64 KiB</button>{:else}<span class="text-text-muted">Full body loaded</span>{/if}
    <span aria-live="polite">{copied}</span>
  </div>
</section>
