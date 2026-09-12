<script lang="ts">
  import { mdiInformationOutline, mdiLoading } from '@mdi/js';
  import { onMount } from 'svelte';
  import { blur } from 'svelte/transition';
  import { APIError, api } from '$lib/api/client';
  import { initialStreamState, openEventStream, reduceStream, type StreamAction, type StreamState } from '$lib/api/events';
  import type { ConfigurationView, ExchangeDetail, ExchangeSummary, OperatingMode, RuntimeStatus, SessionStatus } from '$lib/api/types';
  import { appendOlder, clearHistoryCount, decrementHistoryCount, defaultHistorySelection, formatHistoryLabel, historyTotalCount, mergeNewest, nextHistorySelection } from '$lib/history';
  import { registerServiceWorker, type UpdateController } from '$lib/pwa';
  import { mergeRealtime, summaryFromExchange, type RealtimeExchange } from '$lib/realtime-feed';
  import { formatEndpoint, modeSwitchError, modeSwitchNotice, selectedTransport, selectedTransportStorageKey, udpModeSwitchError, udpModeSwitchNotice, udpTransportDisabledNotice, udpTransportEnabled, type SelectedTransport } from '$lib/settings';
  import { filterByTransport, type TransportFilter } from '$lib/transport-filter';
  import type { Tab } from '$lib/tabs';
  import ConnectionStatus from './ConnectionStatus.svelte';
  import CopyButton from './CopyButton.svelte';
  import ExchangeInspector from './ExchangeInspector.svelte';
  import HistoryList from './HistoryList.svelte';
  import LoginPanel from './LoginPanel.svelte';
  import MdiIcon from './MdiIcon.svelte';
  import PrimaryTabs from './PrimaryTabs.svelte';
  import SecondaryNavigation from './SecondaryNavigation.svelte';
  import Toast, { type ToastMessage } from './Toast.svelte';

  let session = $state<SessionStatus>();
  let runtime = $state<RuntimeStatus>();
  let configuration = $state<ConfigurationView>();
  let stream = $state<StreamState>();
  let active = $state<Tab>('realtime');
  let busy = $state(false);
  let error = $state('');
  let update = $state<UpdateController>();
  let online = $state(true);
  let closeStream = () => {};
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let historyItems = $state<ExchangeSummary[]>([]);
  let historyCursor = $state('');
  let historyHasMore = $state(false);
  let historyLoading = $state(false);
  let historyError = $state('');
  let historySelected = $state<string>();
  let historyDetail = $state<ExchangeDetail>();
  let historyDetailLoading = $state(false);
  let historyDetailError = $state('');
  let liveItems = $state<RealtimeExchange[]>([]);
  let dismissedLiveIDs = $state<string[]>([]);
  let deletingIDs = $state<string[]>([]);
  let liveUnread = $state(false);
  let toastMessages = $state<ToastMessage[]>([]);
  let nextToastID = 0;
  let historyLayout = $state<HTMLElement>();
  let historyPaneWidth = $state(368);
  let transport = $state<SelectedTransport>('http');
  let liveTransportFilter = $state<TransportFilter>('all');
  let historyTransportFilter = $state<TransportFilter>('all');
  let resizingHistory = $state(false);
  const historyPaneStorageKey = 'reqrelay.history-pane-width.v1';
  let historyLabel = $derived(formatHistoryLabel(historyTotalCount(runtime, historyItems.length)));
  let isLive = $derived(stream?.connection === 'live');
  let liveLabel = $derived(isLive ? 'Live' : 'Offline');
  let visibleLiveItems = $derived(liveItems.filter((item) => filterByTransport([item.summary], liveTransportFilter).length > 0));
  let visibleHistoryItems = $derived(filterByTransport(historyItems, historyTransportFilter));

  $effect(() => {
    if (active === 'history' && !historySelected) {
      const defaultId = defaultHistorySelection(historySelected, visibleHistoryItems);
      if (defaultId) void selectHistory(defaultId);
    }
  });

  onMount(() => {
    online = navigator.onLine;
    const storedHistoryWidth = Number(localStorage.getItem(historyPaneStorageKey));
    if (Number.isFinite(storedHistoryWidth)) setHistoryPaneWidth(storedHistoryWidth);
    transport = selectedTransport(localStorage.getItem(selectedTransportStorageKey));
    const requestedTab = new URL(location.href).searchParams.get('tab');
    if (requestedTab === 'history' || requestedTab === 'info' || requestedTab === 'realtime') active = requestedTab;
    const setOnline = () => { online = navigator.onLine; };
    window.addEventListener('online', setOnline);
    window.addEventListener('offline', setOnline);
    window.addEventListener('pointermove', resizeHistoryPane);
    window.addEventListener('pointerup', stopHistoryResize);
    void initialize();
    void registerServiceWorker((available) => { update = available; }).catch(() => { error = 'Service worker registration failed.'; });
    return () => {
      closeStream();
      if (refreshTimer) clearTimeout(refreshTimer);
      window.removeEventListener('online', setOnline);
      window.removeEventListener('offline', setOnline);
      window.removeEventListener('pointermove', resizeHistoryPane);
      window.removeEventListener('pointerup', stopHistoryResize);
    };
  });

  async function initialize(): Promise<void> {
    try {
      session = await api.session();
      if (session.authenticated) await loadApplication();
    } catch (cause) {
      if (cause instanceof TypeError) online = false;
      error = message(cause);
    }
  }

  async function loadApplication(): Promise<void> {
    [runtime, configuration] = await Promise.all([api.status(), api.configuration()]);
    await loadHistory(true);
    stream = initialStreamState();
    closeStream();
    closeStream = openEventStream(handleStream);
  }

  function handleStream(action: StreamAction): void {
    if (!stream) return;
    stream = reduceStream(stream, action);
    if (action.kind === 'snapshot' || action.kind === 'resync') {
      historyItems = mergeNewest(historyItems, action.value.items);
      scheduleRefresh();
    }
    if (action.kind === 'event') {
      if (action.value.type === 'exchange.evicted' && action.value.exchange_id) liveItems = liveItems.filter((item) => item.summary.id !== action.value.exchange_id);
      if (action.value.type === 'exchange.deleted' && action.value.exchange_id) {
        runtime = decrementHistoryCount(runtime);
        removeExchangeFromViews(action.value.exchange_id);
        historyCursor = '';
        void loadHistory(true);
        void refreshLive();
      }
      if (action.value.type === 'history.cleared') {
        runtime = clearHistoryCount(runtime);
        liveItems = [];
        historyItems = [];
        historySelected = undefined;
        historyDetail = undefined;
        historyDetailError = '';
        historyCursor = '';
        void refreshLive();
      }
      if (action.value.exchange_id && ['exchange.started', 'request.completed', 'response.completed', 'exchange.failed'].includes(action.value.type)) {
        void receiveLiveExchange(action.value.exchange_id, action.value.revision);
        if (active !== 'realtime') liveUnread = true;
      }
      if (action.value.type !== 'config.changed') scheduleRefresh();
    }
  }

  function scheduleRefresh(): void {
    if (refreshTimer) return;
    refreshTimer = setTimeout(() => { refreshTimer = undefined; void refreshLive(); }, 80);
  }

  async function refreshLive(): Promise<void> {
    try {
      const status = await api.status();
      runtime = status;
      if (stream) stream = { ...stream, refreshRequired: false };
    } catch (cause) { handleRequestError(cause); }
  }

  async function receiveLiveExchange(id: string, revision: number): Promise<void> {
    if (dismissedLiveIDs.includes(id)) return;
    try {
      const detail = await api.exchange(id);
      if (detail.exchange.revision < revision || dismissedLiveIDs.includes(id)) return;
      const summary = summaryFromExchange(detail.exchange);
      historyItems = mergeNewest(historyItems, [summary]);
      liveItems = mergeRealtime(liveItems, [summary]);
      updateLiveItem(id, summary.revision, (item) => ({ ...item, summary, detail, error: '', unavailable: false }));
    } catch (cause) {
      if (!(cause instanceof APIError && cause.status === 404)) handleRequestError(cause);
    }
  }

  async function loadHistory(reset = false): Promise<void> {
    if (historyLoading) return;
    historyLoading = true;
    historyError = '';
    try {
      const page = await api.exchanges(reset ? '' : historyCursor);
      historyItems = reset ? mergeNewest([], page.items) : appendOlder(historyItems, page.items);
      historyCursor = page.next_cursor ?? '';
      historyHasMore = page.has_more;
    } catch (cause) { historyError = message(cause); }
    finally { historyLoading = false; }
  }

  async function loadLiveDetail(id: string, revision: number): Promise<void> {
    updateLiveItem(id, revision, (item) => ({ ...item, error: '', unavailable: false }));
    try {
      const detail = await api.exchange(id);
      updateLiveItem(id, revision, (item) => ({ ...item, detail }));
    } catch (cause) {
      updateLiveItem(id, revision, (item) => cause instanceof APIError && cause.status === 404
        ? { ...item, unavailable: true }
        : { ...item, error: message(cause) });
    }
  }

  function updateLiveItem(id: string, revision: number, update: (item: RealtimeExchange) => RealtimeExchange): void {
    liveItems = liveItems.map((item) => item.summary.id === id && item.summary.revision === revision ? update(item) : item);
  }

  function dismissLiveExchange(id: string): void {
    if (!dismissedLiveIDs.includes(id)) dismissedLiveIDs = [...dismissedLiveIDs, id];
    liveItems = liveItems.filter((item) => item.summary.id !== id);
    pushToast('Request removed from Realtime.');
  }

  function clearRealtime(): void {
    liveItems = [];
    dismissedLiveIDs = [];
    liveUnread = false;
    pushToast('Realtime cleared.');
  }

  function setHistoryTransportFilter(filter: TransportFilter): void {
    historyTransportFilter = filter;
    if (!historySelected) return;
    const selected = historyItems.find((item) => item.id === historySelected);
    if (selected && !filterByTransport([selected], filter).length) {
      historySelected = undefined;
      historyDetail = undefined;
      historyDetailError = '';
    }
  }

  async function deleteExchange(id: string): Promise<void> {
    if (!confirm('Delete this request from Realtime and History?')) return;
    deletingIDs = [...deletingIDs, id];
    error = '';
    try {
      await api.deleteExchange(id);
      runtime = decrementHistoryCount(runtime);
      removeExchangeFromViews(id);
      void refreshLive();
      pushToast('Request deleted from Realtime and History.');
    } catch (cause) {
      handleRequestError(cause);
    } finally {
      deletingIDs = deletingIDs.filter((value) => value !== id);
    }
  }

  async function clearHistory(): Promise<void> {
    if (!confirm('Clear Realtime and History?')) return;
    busy = true;
    error = '';
    try {
      await api.clearExchanges();
      runtime = clearHistoryCount(runtime);
      liveItems = [];
      historyItems = [];
      historySelected = undefined;
      historyDetail = undefined;
      historyDetailError = '';
      historyCursor = '';
      void refreshLive();
      pushToast('Realtime and History cleared.');
    } catch (cause) {
      handleRequestError(cause);
    } finally {
      busy = false;
    }
  }

  function removeExchangeFromViews(id: string): void {
    liveItems = liveItems.filter((item) => item.summary.id !== id);
    const nextItems = historyItems.filter((item) => item.id !== id);
    historyItems = nextItems;
    dismissedLiveIDs = dismissedLiveIDs.filter((value) => value !== id);
    if (historySelected === id) {
      const nextID = nextHistorySelection(historySelected, id, nextItems);
      historySelected = nextID;
      historyDetail = undefined;
      historyDetailError = '';
      if (nextID) void selectHistory(nextID);
    }
  }

  function pushToast(message: string, tone: ToastMessage['tone'] = 'action'): void {
    const id = nextToastID++;
    toastMessages = [{ id, message, tone }, ...toastMessages];
    setTimeout(() => dismissToast(id), 5000);
  }

  function dismissToast(id: number): void {
    toastMessages = toastMessages.filter((toast) => toast.id !== id);
  }

  function startHistoryResize(event: PointerEvent): void {
    event.preventDefault();
    resizingHistory = true;
  }

  function resizeHistoryPane(event: PointerEvent): void {
    if (!resizingHistory || !historyLayout) return;
    const bounds = historyLayout.getBoundingClientRect();
    setHistoryPaneWidth(event.clientX - bounds.left);
  }

  function stopHistoryResize(): void {
    resizingHistory = false;
  }

  function adjustHistoryPane(event: KeyboardEvent): void {
    const delta = event.key === 'ArrowLeft' ? -16 : event.key === 'ArrowRight' ? 16 : 0;
    if (!delta) return;
    event.preventDefault();
    setHistoryPaneWidth(historyPaneWidth + delta);
  }

  function setHistoryPaneWidth(value: number): void {
    historyPaneWidth = Math.min(560, Math.max(288, Math.round(value)));
    localStorage.setItem(historyPaneStorageKey, String(historyPaneWidth));
  }

  async function selectHistory(id: string): Promise<void> {
    historySelected = id;
    historyDetail = undefined;
    historyDetailLoading = true;
    historyDetailError = '';
    try {
      const detail = await api.exchange(id);
      if (historySelected === id) historyDetail = detail;
    } catch (cause) {
      historyDetailError = cause instanceof APIError && cause.status === 404 ? 'Selected request is no longer available.' : message(cause);
    } finally { historyDetailLoading = false; }
  }

  async function login(username: string, password: string): Promise<void> {
    busy = true;
    error = '';
    try {
      await api.login(username, password);
      session = { login_required: true, authenticated: true };
      await loadApplication();
    } catch (cause) { error = message(cause); }
    finally { busy = false; }
  }

  async function changeMode(targetTransport: SelectedTransport, mode: OperatingMode): Promise<void> {
    if (!configuration || !runtime) return;
    const currentMode = targetTransport === 'udp' ? configuration.effective.udp.mode : runtime.mode;
    if (transport !== targetTransport) {
      changeTransport(targetTransport);
    }
    if (mode === currentMode) return;
    const targetError = targetTransport === 'udp'
      ? udpModeSwitchError(mode, configuration.effective.udp.upstream)
      : modeSwitchError(mode, configuration.effective.upstream.url);
    if (targetError) {
      pushToast(`${targetError} Configure it in Settings before switching.`, 'error');
      return;
    }
    busy = true;
    error = '';
    try {
      if (targetTransport === 'udp') {
        const updated = await api.updateConfiguration(configuration, { 'udp.mode': mode });
        configuration = updated;
        if (updated.udp_reload) runtime = { ...runtime, udp: { ...runtime.udp, enabled: updated.udp_reload.enabled, listener: updated.udp_reload.listener, sessions: 0 }, config_revision: updated.revision };
        pushToast(udpModeSwitchNotice(mode));
      } else {
        configuration = await api.setMode(configuration, mode);
        runtime = { ...runtime, mode: configuration.effective.mode, config_revision: configuration.revision };
        pushToast(modeSwitchNotice(mode, runtime.current_requests));
      }
    } catch (cause) { handleRequestError(cause); }
    finally { busy = false; }
  }

	function changeTransport(next: SelectedTransport): void {
		const disabledNotice = next === 'udp' ? udpTransportDisabledNotice(udpTransportEnabled(runtime?.udp.enabled)) : undefined;
		if (disabledNotice) {
			pushToast(disabledNotice, 'error');
			return;
		}
		transport = next;
		localStorage.setItem(selectedTransportStorageKey, next);
	}

  function handleRequestError(cause: unknown): void {
    if (cause instanceof APIError && cause.status === 401) {
      closeStream();
      session = { login_required: true, authenticated: false };
    }
    error = message(cause);
  }

  function selectTab(tab: Tab): void {
    active = tab;
    if (tab === 'realtime') liveUnread = false;
    const url = new URL(location.href);
    url.searchParams.set('tab', tab);
    history.replaceState({}, '', url);
    if (tab === 'history' && !historySelected) {
      const defaultId = defaultHistorySelection(historySelected, historyItems);
      if (defaultId) void selectHistory(defaultId);
    }
  }

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : 'Unexpected request failure.';
  }
</script>

{#if session?.login_required && !session.authenticated}
  <LoginPanel {busy} {error} onlogin={login} />
{:else}
  <div class="min-h-screen bg-page text-text">
    <header class="border-b border-border bg-surface">
      <div class="mx-auto flex min-h-16 max-w-7xl flex-wrap items-center gap-3 px-3 py-2 sm:px-6">
        <a href="/" class="flex items-center gap-2 text-xl font-bold"><img src="/icons/icon-192.png" alt="" width="36" height="36" class="rounded-lg" /><span>RequestInspector Relay</span></a>
        <ConnectionStatus live={isLive} />
        <div class="ml-auto"><SecondaryNavigation /></div>
      </div>
    </header>
    <PrimaryTabs
      {active}
      {historyLabel}
      {liveLabel}
      {liveUnread}
      liveCount={liveItems.length}
      historyCount={historyTotalCount(runtime, historyItems.length)}
      {transport}
      httpMode={runtime?.mode ?? 'capture'}
      udpMode={configuration?.effective.udp.mode ?? 'capture'}
      httpUpstreamAvailable={Boolean(configuration?.effective.upstream.url?.trim())}
      udpUpstreamAvailable={Boolean(configuration?.effective.udp.upstream?.trim())}
      {busy}
      udpEnabled={udpTransportEnabled(runtime?.udp.enabled)}
      onselect={selectTab}
      ontransportchange={changeTransport}
      onmodechange={changeMode}
      onudpdisabled={() => pushToast(udpTransportDisabledNotice(false) ?? 'UDP disabled.', 'error')}
      onclearRealtime={clearRealtime}
      onclearAll={clearHistory}
    />
    <main class="mx-auto max-w-7xl p-3 sm:p-6">
      {#if error}<p role="alert" class="mb-4 rounded-xl border border-rose-500/30 bg-rose-500/10 p-3.5 text-sm font-medium text-rose-600 dark:text-rose-400 shadow-sm">{error}</p>{/if}
      {#if toastMessages.length}<Toast messages={toastMessages} onclose={dismissToast} />{/if}
      {#if update?.available}
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-4 shadow-sm" role="status">
          <span class="text-sm font-medium text-amber-800 dark:text-amber-300">Update available. Apply when current inspection work is safe to reload.</span>
          <button type="button" class="min-h-10 rounded-xl bg-action px-4 text-sm font-semibold text-white shadow-sm hover:opacity-90 active:scale-95 transition-all" onclick={update.apply}>Apply update</button>
        </div>
      {/if}
      {#if !online || stream?.connection === 'offline'}
        <div class="mb-4 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-4 shadow-sm" role="alert">
          <strong class="font-semibold text-amber-800 dark:text-amber-300">Offline / live data unavailable</strong>
          <p class="mt-1 text-sm text-text-muted">Application shell remains available. Request history and status are never served from offline cache.</p>
        </div>
      {/if}

      {#if active === 'realtime'}
        <div id="panel-realtime" role="tabpanel" aria-labelledby="tab-realtime" class="space-y-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 class="text-2xl font-bold tracking-tight">Realtime</h1>
              <p class="text-xs text-text-muted">Live event stream and incoming traffic inspection.</p>
            </div>
            <fieldset class="flex items-center gap-1 rounded-xl border border-border bg-surface-muted/30 p-1 shadow-sm" aria-label="Realtime transport filter">
              {#each ['all', 'http', 'udp'] as option}
                <button
                  type="button"
                  class="min-h-8 rounded-lg px-3 text-xs font-semibold uppercase tracking-wider transition-all {liveTransportFilter === option ? 'bg-action text-white shadow-sm' : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
                  aria-pressed={liveTransportFilter === option}
                  onclick={() => { liveTransportFilter = option as TransportFilter; }}
                >
                  {option}
                </button>
              {/each}
            </fieldset>
          </div>
          {#if visibleLiveItems.length}
            <div class="space-y-4" aria-label="Realtime requests">
              {#each visibleLiveItems as item (item.summary.id)}
                <div in:blur={{ duration: 300 }}>
                  <ExchangeInspector detail={item.detail} loading={!item.detail && !item.error && !item.unavailable} error={item.error} unavailable={item.unavailable} hideEmptyPayloads deleting={deletingIDs.includes(item.summary.id)} onretry={() => loadLiveDetail(item.summary.id, item.summary.revision)} ondismiss={() => dismissLiveExchange(item.summary.id)} ondelete={() => { void deleteExchange(item.summary.id); }} />
                </div>
              {/each}
            </div>
          {:else}
            <div class="flex flex-col items-center justify-center rounded-2xl border border-border bg-surface p-8 text-center shadow-sm sm:p-12" role="status">
              <div class="feature-icon-wrap mb-4 text-action animate-spin motion-reduce:animate-none">
                <MdiIcon path={mdiLoading} size={24} />
              </div>
              <h2 class="text-base font-semibold text-text">
                {liveItems.length ? 'No requests match this transport filter.' : 'Waiting for first request…'}
              </h2>
              <p class="mt-1 max-w-sm text-xs text-text-muted">
                {liveItems.length ? 'Try selecting another transport filter above or sending requests matching this filter.' : 'Send HTTP or UDP traffic to the active listener endpoints to inspect payloads in realtime.'}
              </p>
            </div>
          {/if}
        </div>
      {:else if active === 'history'}
        <div id="panel-history" role="tabpanel" aria-labelledby="tab-history">
          <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 class="text-2xl font-bold tracking-tight">History</h1>
              <p class="text-xs text-text-muted">Persistent request exchanges stored in database.</p>
            </div>
            <fieldset class="flex items-center gap-1 rounded-xl border border-border bg-surface-muted/30 p-1 shadow-sm" aria-label="History transport filter">
              {#each ['all', 'http', 'udp'] as option}
                <button
                  type="button"
                  class="min-h-8 rounded-lg px-3 text-xs font-semibold uppercase tracking-wider transition-all {historyTransportFilter === option ? 'bg-action text-white shadow-sm' : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
                  aria-pressed={historyTransportFilter === option}
                  onclick={() => setHistoryTransportFilter(option as TransportFilter)}
                >
                  {option}
                </button>
              {/each}
            </fieldset>
          </div>
          <div bind:this={historyLayout} class="grid gap-4 lg:grid-cols-[var(--history-pane-width)_1rem_minmax(0,1fr)] lg:gap-0" style={`--history-pane-width:${historyPaneWidth}px`}>
            <HistoryList items={visibleHistoryItems} selected={historySelected} loading={historyLoading} hasMore={historyHasMore} error={historyError} onselect={(id: string) => { void selectHistory(id); }} onloadmore={() => { void loadHistory(); }} />
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
            <div class="hidden w-4 cursor-col-resize items-stretch justify-center lg:flex group" role="separator" aria-label="Resize History list" aria-orientation="vertical" aria-valuemin="288" aria-valuemax="560" aria-valuenow={historyPaneWidth} tabindex="0" onpointerdown={startHistoryResize} onkeydown={adjustHistoryPane}>
              <span class="w-px bg-border group-hover:bg-action group-focus:bg-action transition-colors"></span>
            </div>
            <ExchangeInspector detail={historyDetail} loading={historyDetailLoading} error={historyDetailError} deleting={historyDetail?.exchange ? deletingIDs.includes(historyDetail.exchange.id) : false} onretry={() => historySelected && selectHistory(historySelected)} ondelete={() => { if (historyDetail?.exchange) void deleteExchange(historyDetail.exchange.id); }} />
          </div>
        </div>
      {:else}
        <div id="panel-info" role="tabpanel" aria-labelledby="tab-info" class="space-y-6">
          <!-- Hero Section / Card -->
          <div class="rounded-2xl border border-border bg-surface p-5 sm:p-8 shadow-sm">
            <div class="flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
              <div class="flex items-center gap-4">
                <div class="feature-icon-wrap text-link">
                  <MdiIcon path={mdiInformationOutline} size={24} />
                </div>
                <div>
                  <h1 class="text-2xl font-bold tracking-tight">Service Endpoints</h1>
                  <p class="mt-1 text-sm text-text-muted">Network listeners and connection URLs for management, HTTP traffic ingestion, and UDP datagrams.</p>
                </div>
              </div>
              {#if runtime}
                <div class="flex items-center gap-2">
                  <span class="rounded-full border border-border bg-surface-muted/50 px-3 py-1 text-xs font-semibold text-text-muted shadow-sm">
                    Uptime: {runtime.uptime_seconds}s
                  </span>
                  <span class="rounded-full border border-link/20 bg-link/10 px-3 py-1 text-xs font-semibold text-link shadow-sm">
                    Revision: {runtime.config_revision}
                  </span>
                </div>
              {/if}
            </div>

            {#if runtime}
              {@const currentHostname = typeof window !== 'undefined' ? window.location.hostname : 'localhost'}
              {@const mgmt = formatEndpoint('http', runtime.management_listener, currentHostname)}
              {@const traffic = formatEndpoint('http', runtime.traffic_listener, currentHostname)}
              {@const udp = formatEndpoint('udp', runtime.udp?.listener, currentHostname)}

              <!-- KPI Stats Grid -->
              <div class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
                  <span class="text-xs font-medium text-text-muted uppercase tracking-wider">Current Requests</span>
                  <p class="mt-1 text-2xl font-bold text-text">{runtime.current_requests}</p>
                </div>
                <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
                  <span class="text-xs font-medium text-text-muted uppercase tracking-wider">HTTP Mode</span>
                  <p class="mt-1 text-2xl font-bold capitalize text-text">{runtime.mode}</p>
                </div>
                <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
                  <span class="text-xs font-medium text-text-muted uppercase tracking-wider">UDP Status</span>
                  <p class="mt-1 text-2xl font-bold text-text">
                    {#if runtime.udp?.enabled}
                      <span class="text-emerald-600 dark:text-emerald-400">Active</span>
                    {:else}
                      <span class="text-text-muted">Disabled</span>
                    {/if}
                  </p>
                </div>
                <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
                  <span class="text-xs font-medium text-text-muted uppercase tracking-wider">Active UDP Sessions</span>
                  <p class="mt-1 text-2xl font-bold text-text">{runtime.udp?.sessions ?? 0}</p>
                </div>
              </div>

              <!-- Endpoints List -->
              <div class="mt-6 space-y-4">
                <!-- Management -->
                {#if mgmt}
                  <div class="rounded-xl border border-border bg-surface-muted/20 p-4 sm:p-5 shadow-sm">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <div class="flex items-center gap-2">
                        <span class="font-bold text-text">Management</span>
                        <span class="rounded-md border border-link/20 bg-link/10 px-2 py-0.5 text-xs font-semibold text-link">HTTP</span>
                      </div>
                      <span class="text-xs text-text-muted">Web Dashboard &amp; REST API</span>
                    </div>
                    <div class="mt-3 flex items-center justify-between gap-2 rounded-xl border border-border bg-surface px-3.5 py-2.5 shadow-sm">
                      <a href={mgmt.url || `http://${mgmt.address}`} target="_blank" rel="noopener noreferrer" class="font-mono text-sm text-link underline break-all hover:text-action">
                        {mgmt.url || `http://${mgmt.address}`}
                      </a>
                      <div class="shrink-0">
                        <CopyButton value={mgmt.copyValue} label="Copy Management URL" />
                      </div>
                    </div>
                  </div>
                {/if}

                <!-- HTTP Ingest -->
                {#if traffic}
                  <div class="rounded-xl border border-border bg-surface-muted/20 p-4 sm:p-5 shadow-sm">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <div class="flex items-center gap-2">
                        <span class="font-bold text-text">HTTP Ingest</span>
                        <span class="rounded-md border border-link/20 bg-link/10 px-2 py-0.5 text-xs font-semibold text-link">HTTP</span>
                        <span class="rounded-md border border-border bg-surface-muted px-2 py-0.5 text-xs font-semibold text-text-muted uppercase">{runtime.mode} mode</span>
                        {#if traffic.isWildcard}
                          <span class="rounded-md border border-border bg-surface-muted px-2 py-0.5 text-xs text-text-muted">All interfaces ({traffic.boundAddress})</span>
                        {/if}
                      </div>
                      <span class="text-xs text-text-muted">Inspection &amp; Reverse Proxy</span>
                    </div>
                    <div class="mt-3 flex items-center justify-between gap-2 rounded-xl border border-border bg-surface px-3.5 py-2.5 shadow-sm">
                      <code class="font-mono text-sm break-all">{traffic.url || `http://${traffic.address}`}</code>
                      <div class="shrink-0">
                        <CopyButton value={traffic.copyValue} label="Copy HTTP Ingest URL" />
                      </div>
                    </div>
                  </div>
                {/if}

                <!-- UDP Ingest -->
                {#if udp}
                  <div class="rounded-xl border border-border bg-surface-muted/20 p-4 sm:p-5 shadow-sm">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <div class="flex items-center gap-2">
                        <span class="font-bold text-text">UDP Ingest</span>
                        <span class="rounded-md border border-border bg-surface-muted px-2 py-0.5 text-xs font-semibold text-text-muted">UDP</span>
                        {#if runtime.udp?.enabled}
                          <span class="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 text-xs font-semibold text-emerald-600 dark:text-emerald-400">Active</span>
                        {:else}
                          <span class="rounded-md border border-border bg-surface-muted px-2 py-0.5 text-xs font-semibold text-text-muted">Disabled</span>
                        {/if}
                        {#if udp.isWildcard}
                          <span class="rounded-md border border-border bg-surface-muted px-2 py-0.5 text-xs text-text-muted">All interfaces ({udp.boundAddress})</span>
                        {/if}
                      </div>
                      <span class="text-xs text-text-muted">Datagram Ingestion</span>
                    </div>
                    <div class="mt-3 flex items-center justify-between gap-2 rounded-xl border border-border bg-surface px-3.5 py-2.5 shadow-sm">
                      <div class="flex items-center gap-2">
                        <span class="rounded bg-surface-muted px-1.5 py-0.5 font-mono text-xs font-bold text-text-muted">UDP</span>
                        <code class="font-mono text-sm font-semibold break-all">{udp.address}</code>
                      </div>
                      <div class="shrink-0">
                        <CopyButton value={udp.copyValue} label="Copy UDP Ingest address" />
                      </div>
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </main>
  </div>
{/if}
