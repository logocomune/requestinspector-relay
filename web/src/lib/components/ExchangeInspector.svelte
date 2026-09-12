<script lang="ts">
  import { mdiCameraOutline, mdiClose, mdiDeleteOutline, mdiHelpCircleOutline, mdiLoading, mdiSwapHorizontalBold } from '@mdi/js';
  import type { ExchangeDetail } from '$lib/api/types';
  import { formatDurationMilliseconds } from '$lib/proxy-timings';
  import CopyButton from './CopyButton.svelte';
  import HeaderTable from './HeaderTable.svelte';
  import MethodBadge from './MethodBadge.svelte';
  import MdiIcon from './MdiIcon.svelte';
  import PayloadViewer from './PayloadViewer.svelte';
  interface Props { detail?: ExchangeDetail; loading?: boolean; error?: string; unavailable?: boolean; hideEmptyPayloads?: boolean; deleting?: boolean; onretry?: () => void; ondismiss?: () => void; ondelete?: () => void }
  let { detail, loading = false, error = '', unavailable = false, hideEmptyPayloads = false, deleting = false, onretry, ondismiss, ondelete }: Props = $props();
  let exchange = $derived(detail?.exchange);
  let httpExchange = $derived(exchange?.transport === 'http' ? exchange : undefined);
  let requestLine = $derived(httpExchange ? `${httpExchange.request.method} ${httpExchange.request.request_target} ${httpExchange.request.protocol}` : '');
  const encoding = (headers: Record<string, string[]>): string => Object.entries(headers).find(([name]) => name.toLowerCase() === 'content-encoding')?.[1].join(', ') ?? '';
</script>

<section class="rounded-2xl border border-border bg-surface shadow-sm overflow-hidden" aria-label="Request inspector">
  {#if loading}
    <div class="flex items-center justify-center gap-2 p-8 text-text-muted" role="status">
      <span class="inline-flex shrink-0 animate-spin text-action motion-reduce:animate-none"><MdiIcon path={mdiLoading} size={20} /></span>
      <span>Loading request…</span>
    </div>
  {:else if unavailable}<p class="p-8 text-center text-text-muted">Selected request is no longer available.</p>
  {:else if error}<p class="p-8 text-center text-error" role="alert">{error} {#if onretry}<button type="button" class="underline" onclick={onretry}>Retry</button>{/if}</p>
  {:else if !exchange}<p class="p-8 text-center text-text-muted">Select a request to view details.</p>
  {:else}
    <header class="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-surface-muted/60 px-4 py-2.5 text-xs text-text-muted">
      <div class="flex flex-wrap items-center gap-2 min-w-0">
        <span class="rounded-md border border-border bg-surface px-2 py-0.5 font-mono text-xs font-semibold text-text shadow-sm">{exchange.transport.toUpperCase()}</span>
        <span class="inline-flex items-center text-text-muted" role="img" aria-label={exchange.mode === 'proxy' ? 'Proxy mode' : 'Capture mode'} title={exchange.mode === 'proxy' ? 'Proxy mode' : 'Capture mode'}>
          <MdiIcon path={exchange.mode === 'proxy' ? mdiSwapHorizontalBold : mdiCameraOutline} size={16} />
        </span>
        <time datetime={exchange.started_at} class="font-mono text-xs text-text">{new Date(exchange.started_at).toLocaleString()}</time>
        <span class="text-text-muted">·</span>
        <span>From <code class="font-mono text-xs text-text">{exchange.transport === 'udp' ? exchange.datagram.source_address : exchange.request.remote_address || 'Unknown'}</code></span>
        {#if ondismiss || ondelete}<span class="text-text-muted font-mono text-[11px] truncate max-w-xs">(ID: {exchange.id})</span>{/if}
      </div>
      {#if ondismiss || ondelete}
        <div class="flex shrink-0 items-center gap-1.5">
          {#if ondelete}<button type="button" class="flex min-h-8 min-w-8 items-center justify-center rounded-lg border border-border bg-surface text-error hover:bg-error/10 shadow-sm transition-colors" aria-label={`Delete ${exchange.id} from Realtime and History`} title="Delete from Realtime and History" disabled={deleting} onclick={ondelete}><MdiIcon path={mdiDeleteOutline} size={16} /></button>{/if}
          {#if ondismiss}<button type="button" class="flex min-h-8 min-w-8 items-center justify-center rounded-lg border border-border bg-surface text-text-muted hover:bg-surface-muted shadow-sm transition-colors" aria-label={`Remove ${exchange.id} from Realtime`} title="Remove from Realtime" disabled={deleting} onclick={ondismiss}><MdiIcon path={mdiClose} size={16} /></button>{/if}
        </div>
      {/if}
    </header>
    {#if httpExchange}
    <div class="p-5 sm:p-6 space-y-4">
      <div>
        <h2 class="text-base font-bold tracking-tight text-text">Request</h2>
        <div class="mt-2 flex flex-wrap items-center gap-2 font-mono text-sm">
          <MethodBadge method={httpExchange.request.method} />
          <code class="min-w-0 break-all font-semibold text-text">{httpExchange.request.request_target} {httpExchange.request.protocol}</code>
          <CopyButton value={requestLine} label="Copy request line" />
        </div>
      </div>
      <HeaderTable headers={httpExchange.request.headers} />
      <p class="text-xs text-text-muted" title="Normalized estimate; raw wire header size may differ.">Request headers estimate: <code class="font-mono">{httpExchange.request.header_bytes_estimate} B</code></p>
      {#if !hideEmptyPayloads || httpExchange.request.observed_body_bytes > 0}
        <PayloadViewer exchangeID={httpExchange.id} side="request" available={detail?.request_available ?? false} contentType={httpExchange.request.content_type}
          encoding={encoding(httpExchange.request.headers)} encodedBytes={httpExchange.request.observed_body_bytes} decodedBytes={httpExchange.request.decoded_preview_bytes}
          truncated={httpExchange.request.preview_truncated} previewError={httpExchange.request.preview_error} />
      {/if}

      {#if httpExchange.mode === 'proxy' && httpExchange.response}
        <hr class="my-6 border-border" />
        <div>
          <h2 class="text-base font-bold tracking-tight text-text">Response</h2>
          <p class="mt-2 font-mono text-sm"><strong class="font-bold text-text">{httpExchange.response.status}</strong> <span class="text-text-muted">· origin</span> <code class="text-xs">{httpExchange.response.origin}</code></p>
        </div>
        <HeaderTable headers={httpExchange.response.headers} />
        <p class="text-xs text-text-muted">Response headers estimate: <code class="font-mono">{httpExchange.response.header_bytes_estimate} B</code></p>
        {#if !hideEmptyPayloads || httpExchange.response.observed_body_bytes > 0}
          <PayloadViewer exchangeID={httpExchange.id} side="response" available={detail?.response_available ?? false} contentType={httpExchange.response.headers['Content-Type']?.[0] ?? ''}
            encoding={encoding(httpExchange.response.headers)} encodedBytes={httpExchange.response.observed_body_bytes} decodedBytes={httpExchange.response.decoded_preview_bytes}
            truncated={httpExchange.response.preview_truncated} previewError={httpExchange.response.preview_error} />
        {/if}
        {#if httpExchange.proxy_timing}
          <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
            <h3 class="text-sm font-bold tracking-tight text-text">Proxy timings</h3>
            <dl class="mt-3 grid grid-cols-[max-content_1fr_max-content_1fr] gap-x-4 gap-y-1.5 text-xs">
              <dt class="flex items-center gap-1 font-medium text-text-muted">Dispatch <span class="inline-flex text-text-muted" role="img" aria-label="Time to send the request upstream, including connection setup when needed." title="Time to send the request upstream, including connection setup when needed."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.upstream_dispatch_duration_us)}</dd>
              <dt class="flex items-center gap-1 font-medium text-text-muted">Wait <span class="inline-flex text-text-muted" role="img" aria-label="Time from the completed request write until the first response byte." title="Time from the completed request write until the first response byte."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.upstream_wait_duration_us)}</dd>
              <dt class="flex items-center gap-1 font-medium text-text-muted">Receive <span class="inline-flex text-text-muted" role="img" aria-label="Time from the first response byte until the complete response is captured." title="Time from the first response byte until the complete response is captured."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.upstream_receive_duration_us)}</dd>
              <dt class="flex items-center gap-1 font-medium text-text-muted">Upstream total <span class="inline-flex text-text-muted" role="img" aria-label="Total time spent from starting the upstream operation until the complete response is captured." title="Total time spent from starting the upstream operation until the complete response is captured."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.upstream_total_duration_us)}</dd>
              <dt class="flex items-center gap-1 font-medium text-text-muted">Downstream write <span class="inline-flex text-text-muted" role="img" aria-label="Time to relay the buffered response to the original client." title="Time to relay the buffered response to the original client."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.downstream_write_duration_us)}</dd>
              <dt class="flex items-center gap-1 font-medium text-text-muted">Proxy total <span class="inline-flex text-text-muted" role="img" aria-label="Total proxy time from admission until downstream completion or failure." title="Total proxy time from admission until downstream completion or failure."><MdiIcon path={mdiHelpCircleOutline} size={14} /></span></dt>
              <dd class="font-mono">{formatDurationMilliseconds(httpExchange.proxy_timing.proxy_total_duration_us)}</dd>
            </dl>
          </div>
        {/if}
        {#if httpExchange.downstream_delivery?.error}<p class="mt-3 text-xs text-error font-semibold">Downstream delivery: {httpExchange.downstream_delivery.error}</p>{/if}
      {/if}
      {#if exchange.error}<p class="mt-4 rounded-xl border border-error bg-error/5 p-3 text-sm text-error" role="alert">{exchange.error.category}: {exchange.error.message}</p>{/if}
    </div>
    {:else if exchange.transport === 'udp'}
      <div class="p-5 sm:p-6 space-y-4">
        <h2 class="text-base font-bold tracking-tight text-text">Datagram</h2>
        <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-sm rounded-xl border border-border bg-surface-muted/20 p-4 shadow-sm">
          <dt class="font-medium text-text-muted text-xs uppercase tracking-wider">Source</dt><dd class="break-all font-mono text-xs">{exchange.datagram.source_address}</dd>
          <dt class="font-medium text-text-muted text-xs uppercase tracking-wider">Listener</dt><dd class="break-all font-mono text-xs">{exchange.datagram.local_address}</dd>
          <dt class="font-medium text-text-muted text-xs uppercase tracking-wider">Accepted bytes</dt><dd class="font-mono text-xs">{exchange.datagram.accepted_bytes}</dd>
          <dt class="font-medium text-text-muted text-xs uppercase tracking-wider">Delivery</dt><dd class="text-xs">{exchange.datagram.delivery.result}</dd>
        </dl>
        {#if exchange.datagram.discard_reason}<p class="rounded-xl border border-error bg-error/5 p-3 text-sm text-error" role="alert">Discarded: {exchange.datagram.discard_reason}</p>{/if}
        {#if exchange.error?.category === 'udp_datagram_oversized'}<p class="rounded-xl border border-error bg-error/5 p-3 text-sm text-error" role="alert">Configured UDP datagram limit rejected this payload.</p>{/if}
        {#if exchange.mode === 'capture' && exchange.datagram.delivery.truncated}<p class="rounded-xl border border-control bg-surface p-3 text-sm text-kpi-text" role="status">Capture reply truncated: {exchange.datagram.delivery.message || 'reply exceeded configured delivery limit.'}</p>{/if}
        {#if detail?.datagram_available}
          <PayloadViewer exchangeID={exchange.id} side="datagram" available={detail.datagram_available} contentType="application/octet-stream" encodedBytes={exchange.datagram.accepted_bytes} truncated={exchange.datagram.preview_truncated} jsonHint />
        {/if}
        {#if exchange.mode === 'proxy'}
          <div class="rounded-xl border border-border bg-surface-muted/20 p-4 shadow-sm">
            <h3 class="text-sm font-bold tracking-tight text-text">Proxy datagram timeline</h3>
            <ol class="mt-2 space-y-1.5 text-xs text-text-muted">
              <li><strong class="text-text">Received</strong> from client</li>
              <li><strong class="text-text">Forwarded upstream</strong> {exchange.state === 'discarded' ? 'not attempted' : 'through client session'}</li>
              <li><strong class="text-text">Upstream reply received</strong> {exchange.datagram.delivery.result === 'not_sent' ? 'not observed' : 'observed for delivery'}</li>
              <li><strong class="text-text">Reply forwarded to client</strong> {exchange.datagram.delivery.result}{exchange.datagram.delivery.sent_bytes ? ` (${exchange.datagram.delivery.sent_bytes} B)` : ''}</li>
            </ol>
          </div>
        {/if}
        {#if exchange.error}<p class="mt-4 rounded-xl border border-error bg-error/5 p-3 text-sm text-error" role="alert">{exchange.error.category}: {exchange.error.message}</p>{/if}
      </div>
    {/if}
  {/if}
</section>
