<script lang="ts">
  import { mdiChevronDown, mdiDeleteOutline } from '@mdi/js';
  import type { OperatingMode } from '$lib/api/types';
  import type { SelectedTransport } from '$lib/settings';
  import type { Tab } from '$lib/tabs';
  import MdiIcon from './MdiIcon.svelte';

  interface Props {
    active: Tab;
    historyLabel: string;
    liveLabel: string;
    liveUnread: boolean;
    liveCount: number;
    historyCount: number;
    transport?: SelectedTransport;
    httpMode?: OperatingMode;
    udpMode?: OperatingMode;
    httpUpstreamAvailable?: boolean;
    udpUpstreamAvailable?: boolean;
    busy?: boolean;
    udpEnabled?: boolean;
    onselect: (tab: Tab) => void;
    ontransportchange?: (transport: SelectedTransport) => void;
    onmodechange?: (transport: SelectedTransport, mode: OperatingMode) => void;
    onudpdisabled?: () => void;
    onclearRealtime?: () => void;
    onclearAll?: () => void;
  }

  let {
    active,
    historyLabel,
    liveLabel,
    liveUnread,
    liveCount,
    historyCount,
    transport = 'http',
    httpMode = 'capture',
    udpMode = 'capture',
    httpUpstreamAvailable = true,
    udpUpstreamAvailable = true,
    busy = false,
    udpEnabled = false,
    onselect,
    ontransportchange,
    onmodechange,
    onudpdisabled,
    onclearRealtime,
    onclearAll
  }: Props = $props();

  let httpMenuOpen = $state(false);
  let httpDetailsEl = $state<HTMLDetailsElement>();
  let udpMenuOpen = $state(false);
  let udpDetailsEl = $state<HTMLDetailsElement>();
  let clearMenuOpen = $state(false);
  let clearDetailsEl = $state<HTMLDetailsElement>();

  let tabs = $derived<Array<{ id: Tab; label: string }>>([
    { id: 'realtime', label: 'Realtime' },
    { id: 'history', label: historyLabel },
    { id: 'info', label: 'Info' }
  ]);

  function move(event: KeyboardEvent, index: number): void {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const direction = event.key === 'ArrowRight' ? 1 : -1;
    const next = (index + direction + tabs.length) % tabs.length;
    onselect(tabs[next].id);
    document.getElementById(`tab-${tabs[next].id}`)?.focus();
  }

  function runClear(action: () => void): void {
    clearMenuOpen = false;
    action();
  }

  function handleWindowPointerDown(event: PointerEvent): void {
    const target = event.target as Node;
    if (httpMenuOpen && httpDetailsEl && !httpDetailsEl.contains(target)) {
      httpMenuOpen = false;
    }
    if (udpMenuOpen && udpDetailsEl && !udpDetailsEl.contains(target)) {
      udpMenuOpen = false;
    }
    if (clearMenuOpen && clearDetailsEl && !clearDetailsEl.contains(target)) {
      clearMenuOpen = false;
    }
  }

  function handleWindowKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      httpMenuOpen = false;
      udpMenuOpen = false;
      clearMenuOpen = false;
    }
  }

  function selectMode(targetTransport: SelectedTransport, targetMode: OperatingMode): void {
    httpMenuOpen = false;
    udpMenuOpen = false;
    onmodechange?.(targetTransport, targetMode);
  }
</script>

<svelte:window onpointerdown={handleWindowPointerDown} onkeydown={handleWindowKeydown} />

<nav class="border-b border-border bg-surface px-3 sm:px-6" aria-label="Primary">
  <div class="mx-auto flex max-w-7xl items-center gap-3">
    <div role="tablist" class="flex min-w-0 gap-1.5 overflow-x-auto py-2">
      {#each tabs as tab, index}
        <button
          id={`tab-${tab.id}`}
          type="button"
          role="tab"
          aria-selected={active === tab.id}
          aria-controls={`panel-${tab.id}`}
          tabindex={active === tab.id ? 0 : -1}
          class="min-h-10 whitespace-nowrap rounded-xl px-4 text-sm font-semibold transition-colors {active === tab.id ? 'border border-action/20 bg-action/10 font-bold text-link' : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
          onclick={() => onselect(tab.id)}
          onkeydown={(event) => move(event, index)}
        >
          {tab.label}
          {#if tab.id === 'realtime'}
            {#if liveUnread}<span class="ml-1 inline-block size-2 rounded-full bg-red-600" aria-label="New realtime request" title="New realtime request"></span>{/if}
            <span class="sr-only">{liveLabel}</span>
          {/if}
        </button>
      {/each}
    </div>

    <div class="ml-auto flex basis-full flex-wrap items-center justify-end gap-2 sm:basis-auto">
      <!-- HTTP Dropdown Button -->
      <details class="relative" bind:open={httpMenuOpen} bind:this={httpDetailsEl}>
        <summary
          class="flex min-h-10 cursor-pointer list-none items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-sm font-semibold select-none shadow-sm transition-colors hover:bg-surface-muted"
          class:bg-action={httpMenuOpen}
          class:text-white={httpMenuOpen}
          title={`HTTP is in ${httpMode} mode`}
          onclick={() => {
            udpMenuOpen = false;
            clearMenuOpen = false;
            if (transport !== 'http') ontransportchange?.('http');
          }}
        >
          <span>HTTP: <span class="capitalize">{httpMode}</span></span>
          <MdiIcon path={mdiChevronDown} size={16} />
        </summary>
        <div class="absolute right-0 z-20 mt-1.5 min-w-44 rounded-xl border border-border bg-surface p-1.5 shadow-lg">
          <div class="px-2.5 py-1 text-xs font-bold text-text-muted uppercase">HTTP Mode</div>
          <button
            type="button"
            class="flex min-h-9 w-full items-center justify-between rounded-lg px-2.5 text-left text-sm font-medium transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
            class:text-action={httpMode === 'capture'}
            class:font-bold={httpMode === 'capture'}
            disabled={busy}
            aria-pressed={httpMode === 'capture'}
            onclick={() => selectMode('http', 'capture')}
          >
            <span>Capture</span>
            {#if httpMode === 'capture'}<span aria-hidden="true">✓</span>{/if}
          </button>
          <button
            type="button"
            class="flex min-h-9 w-full items-center justify-between rounded-lg px-2.5 text-left text-sm font-medium transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
            class:text-action={httpMode === 'proxy'}
            class:font-bold={httpMode === 'proxy'}
            disabled={busy || !httpUpstreamAvailable}
            aria-pressed={httpMode === 'proxy'}
            title={!httpUpstreamAvailable ? 'Configure upstream URL in Settings before selecting Proxy' : ''}
            onclick={() => selectMode('http', 'proxy')}
          >
            <span>Proxy</span>
            {#if httpMode === 'proxy'}<span aria-hidden="true">✓</span>{/if}
          </button>
        </div>
      </details>

      <!-- UDP Dropdown Button -->
      {#if !udpEnabled}
        <button
          type="button"
          class="flex min-h-10 cursor-not-allowed items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-sm font-semibold opacity-60 select-none shadow-sm"
          disabled
          aria-label="UDP"
          aria-disabled="true"
          title="UDP is disabled in Settings"
          onclick={onudpdisabled}
        >
          <span>UDP: Disabled</span>
        </button>
      {:else}
        <details class="relative" bind:open={udpMenuOpen} bind:this={udpDetailsEl}>
          <summary
            class="flex min-h-10 cursor-pointer list-none items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-sm font-semibold select-none shadow-sm transition-colors hover:bg-surface-muted"
            class:bg-action={udpMenuOpen}
            class:text-white={udpMenuOpen}
            title={`UDP is in ${udpMode} mode`}
            onclick={() => {
              httpMenuOpen = false;
              clearMenuOpen = false;
              if (transport !== 'udp') ontransportchange?.('udp');
            }}
          >
            <span>UDP: <span class="capitalize">{udpMode}</span></span>
            <MdiIcon path={mdiChevronDown} size={16} />
          </summary>
          <div class="absolute right-0 z-20 mt-1.5 min-w-44 rounded-xl border border-border bg-surface p-1.5 shadow-lg">
            <div class="px-2.5 py-1 text-xs font-bold text-text-muted uppercase">UDP Mode</div>
            <button
              type="button"
              class="flex min-h-9 w-full items-center justify-between rounded-lg px-2.5 text-left text-sm font-medium transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
              class:text-action={udpMode === 'capture'}
              class:font-bold={udpMode === 'capture'}
              disabled={busy}
              aria-pressed={udpMode === 'capture'}
              onclick={() => selectMode('udp', 'capture')}
            >
              <span>Capture</span>
              {#if udpMode === 'capture'}<span aria-hidden="true">✓</span>{/if}
            </button>
            <button
              type="button"
              class="flex min-h-9 w-full items-center justify-between rounded-lg px-2.5 text-left text-sm font-medium transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
              class:text-action={udpMode === 'proxy'}
              class:font-bold={udpMode === 'proxy'}
              disabled={busy || !udpUpstreamAvailable}
              aria-pressed={udpMode === 'proxy'}
              title={!udpUpstreamAvailable ? 'Configure UDP upstream in Settings before selecting Proxy' : ''}
              onclick={() => selectMode('udp', 'proxy')}
            >
              <span>Proxy</span>
              {#if udpMode === 'proxy'}<span aria-hidden="true">✓</span>{/if}
            </button>
          </div>
        </details>
      {/if}

      <!-- Trash Clear Menu -->
      {#if onclearRealtime && onclearAll}
        <details class="relative" bind:open={clearMenuOpen} bind:this={clearDetailsEl}>
          <summary
            class="flex min-h-10 cursor-pointer list-none items-center justify-center gap-1 rounded-xl border border-border bg-surface px-2.5 text-error shadow-sm transition-colors hover:bg-error/10"
            aria-label="Clear requests"
            title="Clear requests"
            onclick={() => {
              httpMenuOpen = false;
              udpMenuOpen = false;
            }}
          >
            <MdiIcon path={mdiDeleteOutline} size={18} />
            <MdiIcon path={mdiChevronDown} size={14} />
          </summary>
          <div class="absolute right-0 z-20 mt-1.5 min-w-56 rounded-xl border border-border bg-surface p-1.5 shadow-lg">
            <button
              type="button"
              class="block min-h-10 w-full rounded-lg px-3 text-left text-sm font-medium transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
              disabled={liveCount === 0 || busy}
              onclick={() => runClear(onclearRealtime)}
            >
              Clear Realtime
            </button>
            <button
              type="button"
              class="block min-h-10 w-full rounded-lg px-3 text-left text-sm font-medium text-error transition-colors hover:bg-error/10 disabled:cursor-not-allowed disabled:opacity-50"
              disabled={(liveCount === 0 && historyCount === 0) || busy}
              onclick={() => runClear(onclearAll)}
            >
              Clear Realtime and History
            </button>
          </div>
        </details>
      {/if}
    </div>
  </div>
</nav>
