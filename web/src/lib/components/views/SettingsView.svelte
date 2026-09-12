<script lang="ts">
  import { onMount } from 'svelte';
  import { APIError, api } from '$lib/api/client';
  import type { CleanupPreview, ConfigurationView, RuntimeStatus, SessionStatus, StorageStatus } from '$lib/api/types';
  import { configurationFields, draftConfiguration, httpSections, managementDisplayURL, managementNavigationURL, modeSwitchError, modeSwitchNotice, normalizeAuthenticationOverrides, normalizeSelectOptions, parseDrafts, settingsTabs, type SettingsSection, type SettingsTab, udpFieldDisabled, udpModeSwitchError, udpSections } from '$lib/settings';
  import { mdiArrowLeft, mdiCogOutline, mdiEyeOffOutline, mdiEyeOutline } from '@mdi/js';
  import LoginPanel from '../LoginPanel.svelte';
  import MdiIcon from '../MdiIcon.svelte';
  import PublicHeader from '../PublicHeader.svelte';
  import ThemeSelector from '../ThemeSelector.svelte';
  import Toast, { type ToastMessage } from '../Toast.svelte';

  let session = $state<SessionStatus>();
  let configuration = $state<ConfigurationView>();
  let runtime = $state<RuntimeStatus>();
  let storage = $state<StorageStatus>();
  let drafts = $state<Record<string, string>>({});
  let dirty = $state<Set<string>>(new Set());
  let removedOverrides = $state<Set<string>>(new Set());
  let fieldErrors = $state<Record<string, string>>({});
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let keepDays = $state(30);
  let cleanupPlan = $state<CleanupPreview>();
  let vacuumAcknowledged = $state(false);
  let toastMessages = $state<ToastMessage[]>([]);
  let nextToastID = 0;
  let activeTab = $state<SettingsTab>('HTTP');
  let passwordVisible = $state(false);

  onMount(() => { void initialize(); });

  async function initialize(): Promise<void> {
    error = '';
    try {
      session = await api.session();
      if (session.authenticated) await loadProtected();
    } catch (cause) { error = message(cause); }
  }

  async function loadProtected(): Promise<void> {
    [configuration, runtime] = await Promise.all([api.configuration(), api.status()]);
    drafts = draftConfiguration(configuration);
    dirty = new Set();
    removedOverrides = new Set();
    await refreshStorage();
  }

  async function login(username: string, password: string): Promise<void> {
    busy = true;
    error = '';
    try {
      await api.login(username, password);
      session = { login_required: true, authenticated: true };
      await loadProtected();
    } catch (cause) { error = message(cause); }
    finally { busy = false; }
  }

  function setDraft(path: string, event: Event): void {
    drafts[path] = (event.currentTarget as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value;
    dirty = new Set(dirty).add(path);
    const nextErrors = { ...fieldErrors };
    delete nextErrors[path];
    fieldErrors = nextErrors;
  }

  function stageOverrideRemoval(field: string): void {
    const fields = field.startsWith('authentication.') ? ['authentication.username', 'authentication.password'] : [field];
    removedOverrides = new Set([...removedOverrides, ...fields]);
    dirty = new Set([...dirty].filter((path) => !fields.includes(path)));
    const nextErrors = { ...fieldErrors };
    for (const path of fields) delete nextErrors[path];
    fieldErrors = nextErrors;
  }

  function fieldsFor(tab: SettingsTab, section?: SettingsSection) {
    return configurationFields.filter((field) => field.tab === tab && field.section === section);
  }

  function sectionsFor(tab: SettingsTab): Array<SettingsSection | undefined> {
    if (tab === 'HTTP') return [undefined, ...httpSections];
    if (tab === 'UDP') return [undefined, ...udpSections];
    if (tab === 'Maintenance') return [];
    return [undefined];
  }

  function tabHasErrors(tab: SettingsTab): boolean {
    return configurationFields.some((field) => field.tab === tab && fieldErrors[field.path]);
  }

  async function save(): Promise<void> {
    if (!configuration || (dirty.size === 0 && removedOverrides.size === 0)) return;
    const parsed = parseDrafts(drafts, dirty);
    fieldErrors = parsed.errors;
    if (Object.keys(parsed.errors).length > 0) return;
    const overrides = normalizeAuthenticationOverrides(drafts, dirty, parsed.overrides);
    const mode = overrides.mode ?? configuration.effective.mode;
    const upstream = overrides['upstream.url'] ?? configuration.effective.upstream.url;
    const targetError = modeSwitchError(mode as 'capture' | 'proxy', String(upstream));
    if (targetError) {
      fieldErrors = { 'upstream.url': targetError };
      return;
    }
		const udpMode = overrides['udp.mode'] ?? configuration.effective.udp.mode;
		const udpUpstream = overrides['udp.upstream'] ?? configuration.effective.udp.upstream;
		const udpTargetError = udpModeSwitchError(udpMode as 'capture' | 'proxy', String(udpUpstream));
		if (udpTargetError) {
			fieldErrors = { 'udp.upstream': udpTargetError };
			return;
		}
    const modeChanged = 'mode' in overrides && overrides.mode !== configuration.effective.mode;
    busy = true;
    error = '';
    notice = '';
    try {
      const updated = await api.updateConfiguration(configuration, overrides, [...removedOverrides]);
      configuration = updated;
      drafts = draftConfiguration(configuration);
      dirty = new Set();
      removedOverrides = new Set();
      notice = `Saved configuration revision ${configuration.revision}.`;
      if (updated.management_reload) {
        const reload = updated.management_reload;
        const displayURL = managementDisplayURL(reload.listener, window.location.hostname);
        if (reload.address_changed) {
          const navigationURL = managementNavigationURL({ current: new URL(window.location.href), previousListener: runtime?.management_listener ?? '', nextListener: reload.listener });
          notice = navigationURL
            ? `Management interface moved to ${navigationURL}. Redirecting…`
            : `Management interface moved${displayURL ? ` to ${displayURL}` : ` to ${reload.listener}`}. Open the new endpoint to continue.`;
          pushToast('Management interface reloaded on the new listener.');
          if (navigationURL) setTimeout(() => window.location.assign(navigationURL), 750);
          if (reload.relogin_required) session = { login_required: true, authenticated: false };
          return;
        }
        if (reload.relogin_required) {
          session = { login_required: true, authenticated: false };
          pushToast('Management authentication reloaded. Sign in again.');
          return;
        }
        pushToast('Management interface reloaded.');
      }
      runtime = await api.status();
      if (updated.traffic_reload) pushToast(`HTTP ingest reloaded on ${updated.traffic_reload.listener}.`);
      if (updated.udp_reload) pushToast(updated.udp_reload.enabled ? `UDP ingest reloaded on ${updated.udp_reload.listener}.` : 'UDP ingest disabled.');
      else if (!updated.management_reload && !updated.traffic_reload) pushToast(modeChanged ? modeSwitchNotice(mode as 'capture' | 'proxy', runtime.current_requests) : 'Settings saved.');
      await refreshStorage();
    } catch (cause) {
      if (cause instanceof APIError && cause.code === 'revision_conflict') {
        await loadProtected();
        error = 'Configuration changed elsewhere. Latest values reloaded; review and save again.';
      } else {
        error = message(cause);
        if (cause instanceof APIError && (cause.code === 'management_listener_unavailable' || cause.code === 'traffic_listener_unavailable' || cause.code === 'udp_listener_unavailable')) pushToast(error, 'error');
      }
    } finally { busy = false; }
  }

  async function refreshStorage(): Promise<void> {
    cleanupPlan = undefined;
    if (!runtime?.storage?.enabled) { storage = undefined; return; }
    try { storage = await api.sqliteStatus(); }
    catch (cause) { error = message(cause); }
  }

  async function previewCleanup(): Promise<void> {
    busy = true;
    error = '';
    notice = '';
    try { cleanupPlan = await api.cleanupPreview(keepDays); }
    catch (cause) { error = message(cause); }
    finally { busy = false; }
  }

  async function cleanup(): Promise<void> {
    if (!cleanupPlan || !confirm(`Delete ${cleanupPlan.exchange_count} exchange(s) completed before ${new Date(cleanupPlan.cutoff).toLocaleString()}?`)) return;
    await runMaintenance('Cleanup', async () => {
      const result = await api.cleanup(keepDays);
      return `Cleanup completed: ${result.deleted} exchange(s) deleted.`;
    });
  }

  async function compact(): Promise<void> {
    await runMaintenance('Compaction', async () => {
      const result = await api.compact();
      return `Compaction completed: ${formatBytes(result.bytes_reclaimed)} reclaimed.`;
    });
  }

  async function vacuum(): Promise<void> {
    if (!vacuumAcknowledged || !confirm('Full VACUUM blocks storage admission, requires free disk space, and cannot be cancelled safely after SQLite starts rebuilding. Continue?')) return;
    await runMaintenance('Full VACUUM', async () => {
      await api.vacuum();
      vacuumAcknowledged = false;
      return 'Full VACUUM completed and database integrity verified.';
    });
  }

  async function runMaintenance(name: string, action: () => Promise<string>): Promise<void> {
    busy = true;
    error = '';
    notice = `${name} running…`;
    try {
      notice = await action();
      await refreshStorage();
      pushToast(notice);
    } catch (cause) {
      notice = '';
      error = message(cause);
      pushToast(error, 'error');
    }
    finally { busy = false; }
  }

  function formatBytes(value: number): string {
    if (value < 1024) return `${value} B`;
    return `${(value / 1024).toFixed(1)} KiB`;
  }

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : 'Unexpected request failure.';
  }

  function pushToast(message: string, tone: ToastMessage['tone'] = 'action'): void {
    const id = nextToastID++;
    toastMessages = [{ id, message, tone }, ...toastMessages];
    setTimeout(() => dismissToast(id), 5000);
  }

  function dismissToast(id: number): void {
    toastMessages = toastMessages.filter((toast) => toast.id !== id);
  }
</script>

<PublicHeader />
{#if toastMessages.length}<Toast messages={toastMessages} onclose={dismissToast} />{/if}
{#if session?.login_required && !session.authenticated}
  <LoginPanel {busy} {error} onlogin={login} />
{:else}
  <main class="mx-auto max-w-5xl p-3 sm:p-6">
    <div class="rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-8">
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
        <div class="flex items-center gap-3.5">
          <div class="feature-icon-wrap flex items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
            <MdiIcon path={mdiCogOutline} size={24} />
          </div>
          <div>
            <h1 class="text-2xl font-bold tracking-tight sm:text-3xl">Settings</h1>
            <p class="mt-0.5 text-sm text-text-muted">Persistent UI overrides take precedence over file, environment, CLI, and defaults</p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <ThemeSelector />
          <a
            href="/"
            class="inline-flex min-h-11 items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-1.5 text-sm font-medium text-text-muted transition-colors hover:bg-surface-muted hover:text-text"
            aria-label="Back to Realtime"
            title="Back to Realtime"
          >
            <MdiIcon path={mdiArrowLeft} size={18} />
            <span>Back</span>
          </a>
        </div>
      </div>

      {#if error}
        <div role="alert" class="mb-6 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-sm font-medium text-red-600 dark:text-red-400 shadow-sm">
          {error}
        </div>
      {/if}
      {#if notice}
        <div role="status" class="mb-6 rounded-xl border border-blue-500/30 bg-blue-500/10 p-3.5 text-sm font-medium text-blue-600 dark:text-blue-400 shadow-sm">
          {notice}
        </div>
      {/if}

      {#if configuration}
        <form class="space-y-6" onsubmit={(event) => { event.preventDefault(); void save(); }} novalidate>
          <div class="flex gap-2 overflow-x-auto border-b border-border pb-3" role="tablist" aria-label="Configuration sections">
            {#each settingsTabs as tab}
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === tab}
                aria-controls={`settings-${tab.toLowerCase().replaceAll(' ', '-')}`}
                class={activeTab === tab
                  ? 'relative shrink-0 rounded-lg border border-action/20 bg-action/10 px-3.5 py-2 text-sm font-bold text-link shadow-sm'
                  : 'relative shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold text-text-muted transition-colors hover:bg-surface-muted hover:text-text'}
                onclick={() => activeTab = tab}
              >
                {tab}
                {#if tabHasErrors(tab)}<span class="ml-1 inline-block h-2 w-2 rounded-full bg-error" aria-label="Validation errors"></span>{/if}
              </button>
            {/each}
          </div>

          {#if activeTab !== 'Maintenance'}
          <div id={`settings-${activeTab.toLowerCase().replaceAll(' ', '-')}`} role="tabpanel" aria-label={activeTab} class="rounded-xl border border-border bg-surface-muted/20 p-5 shadow-sm">
            <h2 class="px-1 text-xl font-bold tracking-tight">{activeTab}</h2>
            {#each sectionsFor(activeTab) as section}
              {#if section}<h3 class="mt-6 border-b border-border px-1 pb-2 text-base font-bold text-text first:mt-2">{section}</h3>{/if}
              <div class="mt-4 grid gap-4 md:grid-cols-2">
                {#each fieldsFor(activeTab, section) as field}
                  <div class:md:col-span-2={field.kind === 'textarea' || field.kind === 'json'} class="rounded-xl border border-border bg-surface p-4 shadow-sm">
                    <div class="flex flex-wrap items-center gap-2">
                      <label for={`setting-${field.path}`} class="font-semibold text-sm">{field.label}</label>
                      <span class="rounded-md bg-surface-muted px-2 py-0.5 text-xs font-medium uppercase text-text-muted">{configuration.origins[field.path] ?? 'default'}</span>
                      {#if configuration.overrides.includes(field.path) && !removedOverrides.has(field.path)}<span class="rounded-md border border-blue-500/20 bg-blue-500/10 px-2 py-0.5 text-xs font-medium text-blue-600 dark:text-blue-400">UI override</span>{/if}
                      {#if removedOverrides.has(field.path)}<span class="rounded-md border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">Will reset</span>{/if}
                      {#if configuration.pending_restart.includes(field.path)}<span class="rounded-md border border-purple-500/20 bg-purple-500/10 px-2 py-0.5 text-xs font-medium text-purple-600 dark:text-purple-400">Restart required</span>{/if}
                    </div>
                    <div class="relative mt-2">
                      {#if field.kind === 'textarea' || field.kind === 'json'}
                        <textarea id={`setting-${field.path}`} rows={field.kind === 'json' ? 5 : 3} value={drafts[field.path]} oninput={(event) => setDraft(field.path, event)} aria-describedby={`help-${field.path}`} aria-invalid={fieldErrors[field.path] ? 'true' : undefined} disabled={udpFieldDisabled(field.path, drafts['udp.enabled'] === 'true')} class="w-full rounded-xl border border-border bg-surface-muted/30 p-2.5 pr-12 font-mono text-sm disabled:cursor-not-allowed disabled:bg-surface-muted disabled:text-text-muted disabled:opacity-70"></textarea>
                      {:else if field.kind === 'select' || field.kind === 'boolean'}
                        <select id={`setting-${field.path}`} value={drafts[field.path]} onchange={(event) => setDraft(field.path, event)} aria-describedby={`help-${field.path}`} disabled={udpFieldDisabled(field.path, drafts['udp.enabled'] === 'true')} class="min-h-11 w-full rounded-xl border border-border bg-surface-muted/30 px-3 pr-12 text-sm disabled:cursor-not-allowed disabled:bg-surface-muted disabled:text-text-muted disabled:opacity-70">
                          {#each field.kind === 'boolean' ? [{ value: 'true', label: 'true' }, { value: 'false', label: 'false' }] : normalizeSelectOptions(field.options, drafts[field.path]) as option}
                            <option value={option.value}>{option.label}</option>
                          {/each}
                        </select>
                      {:else}
                        <input id={`setting-${field.path}`} type={field.kind === 'integer' ? 'number' : field.kind === 'password' && !passwordVisible ? 'password' : 'text'} value={drafts[field.path]} oninput={(event) => setDraft(field.path, event)} aria-describedby={`help-${field.path}`} aria-invalid={fieldErrors[field.path] ? 'true' : undefined} disabled={udpFieldDisabled(field.path, drafts['udp.enabled'] === 'true')} class="min-h-11 w-full rounded-xl border border-border bg-surface-muted/30 px-3 pr-20 text-sm disabled:cursor-not-allowed disabled:bg-surface-muted disabled:text-text-muted disabled:opacity-70" autocomplete={field.kind === 'password' ? 'new-password' : 'off'} />
                      {/if}
                      {#if field.kind === 'password'}<button type="button" class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-2 text-link hover:bg-surface-muted focus:outline-none focus:ring-2 focus:ring-action" aria-label={passwordVisible ? 'Hide password' : 'Show password'} title={passwordVisible ? 'Hide password' : 'Show password'} onclick={() => { passwordVisible = !passwordVisible; }}><MdiIcon path={passwordVisible ? mdiEyeOffOutline : mdiEyeOutline} size={18} /></button>{/if}
                      {#if configuration.overrides.includes(field.path) && !removedOverrides.has(field.path)}<button type="button" class="absolute right-10 top-1/2 -translate-y-1/2 rounded-lg px-2 py-1 text-xl font-bold leading-none text-error hover:bg-error/10 focus:outline-none focus:ring-2 focus:ring-error" disabled={busy} aria-label="Reset setting" title="Reset to lower-priority value on Save" onclick={() => stageOverrideRemoval(field.path)}>×</button>{/if}
                    </div>
                    <p id={`help-${field.path}`} class="mt-1.5 text-xs text-text-muted leading-relaxed">{field.help}</p>
                    {#if fieldErrors[field.path]}<p class="mt-1 text-xs font-semibold text-error" role="alert">{fieldErrors[field.path]}</p>{/if}
                  </div>
                {/each}
              </div>
            {/each}
          </div>

          {#if activeTab === 'Storage'}
            <section class="mt-8 rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="configuration-file">
              <h2 id="configuration-file" class="text-xl font-bold tracking-tight">Configuration file</h2>
              <label for="configuration-file-path" class="mt-4 block text-xs font-semibold uppercase tracking-wider text-text-muted">Active path</label>
              <input id="configuration-file-path" type="text" readonly value={configuration.config_path} class="mt-1 min-h-11 w-full rounded-xl border border-border bg-surface-muted/30 px-3 font-mono text-sm text-text-muted" aria-describedby="configuration-file-help" />
              <p id="configuration-file-help" class="mt-1.5 text-xs text-text-muted">Read-only path selected at startup. Change it with the command-line option or environment variable, then restart.</p>
            </section>
          {/if}

          <div class="sticky bottom-4 z-10 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-surface/95 backdrop-blur-md p-4 shadow-lg">
            <span class="text-sm font-medium text-text-muted">Revision <strong class="font-mono text-text">{configuration.revision}</strong> · {dirty.size + removedOverrides.size} changed field(s)</span>
            <button type="submit" class="min-h-11 rounded-xl bg-action px-6 font-semibold text-white shadow-sm transition-colors hover:bg-action/90 disabled:opacity-50" disabled={busy || (dirty.size === 0 && removedOverrides.size === 0)}>Save settings</button>
          </div>
          {/if}
        </form>
      {/if}

      {#if storage && activeTab === 'Maintenance'}
        <section class="mt-8 rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="storage-maintenance">
          <h2 id="storage-maintenance" class="text-xl font-bold tracking-tight">SQLite storage and maintenance</h2>
          <dl class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Database</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text break-all">{storage.path}</dd>
            </div>
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Size</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text">{formatBytes(storage.database_bytes)} + {formatBytes(storage.wal_bytes)} WAL</dd>
            </div>
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Exchanges</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text">{storage.exchange_count}</dd>
            </div>
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Writer queue</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text">{storage.queue_count}/{storage.queue_capacity}; {formatBytes(storage.reserved_bytes)} reserved</dd>
            </div>
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Maintenance</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text">{storage.maintenance || 'idle'}</dd>
            </div>
            <div class="rounded-xl border border-border bg-surface-muted/30 p-3.5 shadow-sm">
              <dt class="text-xs font-semibold uppercase tracking-wider text-text-muted">Failures</dt>
              <dd class="mt-1 font-mono text-sm font-semibold text-text">{storage.write_failures} write; {storage.busy_failures} busy</dd>
            </div>
          </dl>
          <div class="mt-6 grid gap-4 md:grid-cols-3">
            <section class="rounded-xl border border-border bg-surface-muted/20 p-4 shadow-sm flex flex-col justify-between">
              <div>
                <h3 class="text-base font-bold tracking-tight">Retention cleanup</h3>
                <label class="mt-2.5 block text-xs font-semibold uppercase tracking-wider text-text-muted" for="keep-days">Keep days</label>
                <input id="keep-days" type="number" min="0" bind:value={keepDays} class="mt-1 min-h-11 w-full rounded-xl border border-border bg-surface px-3 font-mono text-sm" />
              </div>
              <div class="mt-4">
                <button type="button" class="min-h-11 w-full rounded-xl border border-border bg-surface px-4 font-semibold text-link shadow-sm transition-colors hover:bg-surface-muted" disabled={busy || keepDays < 0} onclick={() => { void previewCleanup(); }}>Preview cleanup</button>
                {#if cleanupPlan}
                  <div class="mt-3 rounded-lg border border-blue-500/20 bg-blue-500/10 p-2.5 text-xs text-blue-700 dark:text-blue-300" role="status">
                    Cleanup preview: {cleanupPlan.exchange_count} exchange(s), cutoff {new Date(cleanupPlan.cutoff).toLocaleString()}.
                  </div>
                  <button type="button" class="mt-2 min-h-11 w-full rounded-xl bg-action px-4 font-semibold text-white shadow-sm transition-colors hover:bg-action/90" disabled={busy} onclick={() => { void cleanup(); }}>Delete previewed exchanges</button>
                {/if}
              </div>
            </section>
            <section class="rounded-xl border border-border bg-surface-muted/20 p-4 shadow-sm flex flex-col justify-between">
              <div>
                <h3 class="text-base font-bold tracking-tight">Incremental compaction</h3>
                <p class="mt-2 text-sm leading-relaxed text-text-muted">Checkpoints WAL and reclaims a bounded page batch.</p>
              </div>
              <div class="mt-4">
                <button type="button" class="min-h-11 w-full rounded-xl border border-border bg-surface px-4 font-semibold text-link shadow-sm transition-colors hover:bg-surface-muted" disabled={busy} onclick={() => { void compact(); }}>Run compaction</button>
              </div>
            </section>
            <section class="rounded-xl border border-red-500/30 bg-red-500/5 p-4 shadow-sm flex flex-col justify-between">
              <div>
                <h3 class="text-base font-bold tracking-tight text-red-600 dark:text-red-400">Full VACUUM</h3>
                <p class="mt-2 text-sm leading-relaxed text-text-muted">Advanced full rebuild. New traffic may receive HTTP 503 during maintenance.</p>
                <label class="mt-3 flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" bind:checked={vacuumAcknowledged} class="rounded" /> I understand operational impact</label>
              </div>
              <div class="mt-4">
                <button type="button" class="min-h-11 w-full rounded-xl border border-red-500/40 bg-surface px-4 font-semibold text-error shadow-sm transition-colors hover:bg-red-500/10" disabled={busy || !vacuumAcknowledged} onclick={() => { void vacuum(); }}>Run full VACUUM</button>
              </div>
            </section>
          </div>
        </section>
      {/if}
    </div>
  </main>
{/if}
