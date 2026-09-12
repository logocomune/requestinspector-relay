<script lang="ts">
  import { mdiCameraOutline, mdiLoading, mdiSwapHorizontalBold } from '@mdi/js';
  import type { ExchangeSummary } from '$lib/api/types';
  import { visibleWindow } from '$lib/history';
  import MethodBadge from './MethodBadge.svelte';
  import MdiIcon from './MdiIcon.svelte';

  interface Props {
    items: ExchangeSummary[];
    selected?: string;
    loading: boolean;
    hasMore: boolean;
    error?: string;
    onselect: (id: string) => void;
    onloadmore: () => void;
  }

  let {
    items,
    selected,
    loading,
    hasMore,
    error,
    onselect,
    onloadmore
  }: Props = $props();

  let scrollTop = $state(0);
  let viewportHeight = $state(448);
  let window = $derived(visibleWindow(scrollTop, viewportHeight, items.length, 80));

  function scroll(event: Event): void {
    const target = event.currentTarget as HTMLDivElement;
    scrollTop = target.scrollTop;
    viewportHeight = target.clientHeight;
    if (hasMore && !loading && target.scrollHeight - target.scrollTop - target.clientHeight < 160) onloadmore();
  }

  function modeIcon(mode: ExchangeSummary['mode']): string {
    return mode === 'proxy' ? mdiSwapHorizontalBold : mdiCameraOutline;
  }

  function modeLabel(mode: ExchangeSummary['mode']): string {
    return mode === 'proxy' ? 'Proxy mode' : 'Capture mode';
  }
</script>

<section class="overflow-hidden rounded-2xl border border-border bg-surface shadow-sm" aria-labelledby="history-list-heading">
  <div class="flex items-center justify-between border-b border-border bg-surface-muted/30 px-4 py-3">
    <h2 id="history-list-heading" class="text-sm font-bold tracking-tight text-text">History List</h2>
    <span class="rounded-full border border-border bg-surface px-2.5 py-0.5 text-xs font-semibold text-text-muted">
      {items.length} {items.length === 1 ? 'item' : 'items'}
    </span>
  </div>
  {#if items.length === 0 && !loading}
    <p class="p-8 text-center text-sm text-text-muted">No requests captured.</p>
  {/if}
  <div class="h-[28rem] overflow-y-auto" role="listbox" aria-label="Captured requests" onscroll={scroll}>
    <div class="relative" style={`height:${window.total}px`}>
      <div class="absolute inset-x-0" style={`transform:translateY(${window.offset}px)`}>
        {#each items.slice(window.start, window.end) as item (item.id)}
          <button
            type="button"
            role="option"
            aria-selected={selected === item.id}
            onclick={() => onselect(item.id)}
            class="flex h-20 w-full items-center gap-3 border-b border-border/70 px-4 text-left transition-colors {selected === item.id ? 'bg-action text-white' : 'bg-surface hover:bg-surface-muted/50 text-text'}"
          >
            {#if item.transport === 'udp'}
              <span class="rounded-md px-2 py-1 font-mono text-xs font-semibold {selected === item.id ? 'bg-white/20 text-white' : 'bg-surface-muted text-text-muted'}">UDP</span>
            {:else}
              <span class="rounded-md px-2 py-1 font-mono text-xs font-semibold {selected === item.id ? 'bg-white/20 text-white' : 'bg-surface-muted text-text-muted'}">HTTP</span>
            {/if}
            <span class="inline-flex shrink-0 {selected === item.id ? 'text-white/80' : 'text-text-muted'}" role="img" aria-label={modeLabel(item.mode)} title={modeLabel(item.mode)}>
              <MdiIcon path={modeIcon(item.mode)} size={18} />
            </span>
            <span class="min-w-0 flex-1">
              {#if item.transport === 'udp'}
                <span class="block truncate font-mono text-sm font-medium">{item.source_address} → {item.local_address}</span>
                <span class="block truncate text-xs {selected === item.id ? 'text-white/80' : 'text-text-muted'}">{item.datagram_bytes} B · {item.mode} · {item.state} · {new Date(item.started_at).toLocaleString()}</span>
              {:else}
                <span class="block truncate font-mono text-sm font-medium"><MethodBadge method={item.method} /> <span class="align-middle">{item.path || '/'}</span></span>
                <span class="block text-xs {selected === item.id ? 'text-white/80' : 'text-text-muted'}">{new Date(item.started_at).toLocaleString()}</span>
              {/if}
            </span>
          </button>
        {/each}
      </div>
    </div>
  </div>
  <div class="border-t border-border bg-surface-muted/20 p-3 text-center text-xs font-medium text-text-muted" role="status">
    {#if loading}
      <span class="inline-flex items-center gap-2">
        <span class="inline-flex shrink-0 animate-spin text-action motion-reduce:animate-none"><MdiIcon path={mdiLoading} size={16} /></span>
        <span>Loading history…</span>
      </span>
    {:else if error}
      <button type="button" class="text-link underline hover:text-action" onclick={onloadmore}>Retry: {error}</button>
    {:else if hasMore}
      Scroll for older requests
    {:else if items.length}
      End of history
    {/if}
  </div>
</section>
