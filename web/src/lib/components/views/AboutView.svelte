<script lang="ts">
  import { onMount } from 'svelte';
  import {
    mdiArrowLeft,
    mdiBugOutline,
    mdiCalendarClock,
    mdiEarth,
    mdiGithub,
    mdiInformationOutline,
    mdiLicense,
    mdiOpenInNew,
    mdiShieldCheckOutline,
    mdiSourceBranch,
    mdiTagOutline
  } from '@mdi/js';
  import { api } from '$lib/api/client';
  import type { RuntimeStatus } from '$lib/api/types';
  import MdiIcon from '../MdiIcon.svelte';
  import PublicHeader from '../PublicHeader.svelte';

  let runtime = $state<RuntimeStatus>();

  onMount(() => {
    void api.session()
      .then((session) => (session.authenticated ? api.status() : undefined))
      .then((status) => {
        runtime = status;
      })
      .catch(() => {});
  });
</script>

<PublicHeader />
<main class="mx-auto max-w-4xl p-3 sm:p-6">
  <article class="space-y-6">
    <section class="rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-8" aria-labelledby="about-title">
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
        <div class="flex items-center gap-3.5">
          <div class="feature-icon-wrap flex items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
            <MdiIcon path={mdiInformationOutline} size={24} />
          </div>
          <div>
            <h1 id="about-title" class="text-2xl font-bold tracking-tight sm:text-3xl">About RequestInspector Relay</h1>
            <p class="mt-0.5 text-sm text-text-muted">Self-hosted HTTP and UDP request inspector and reverse proxy</p>
          </div>
        </div>
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

      <p class="mb-6 text-sm text-text-muted sm:text-base">
        RequestInspector Relay captures, proxies, and inspects HTTP/1.1 and UDP traffic locally with deep payload inspection, SQLite persistence, and zero external runtime dependencies.
      </p>

      <dl class="grid gap-3.5 sm:grid-cols-3" aria-label="Build information">
        <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
          <div class="mb-1.5 flex items-center gap-2 text-text-muted">
            <MdiIcon path={mdiTagOutline} size={16} />
            <dt class="text-xs font-semibold uppercase tracking-wider">Version</dt>
          </div>
          <dd class="font-mono text-sm font-semibold text-text break-all">{runtime?.build || 'Unknown'}</dd>
        </div>
        <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
          <div class="mb-1.5 flex items-center gap-2 text-text-muted">
            <MdiIcon path={mdiSourceBranch} size={16} />
            <dt class="text-xs font-semibold uppercase tracking-wider">Commit</dt>
          </div>
          <dd class="font-mono text-sm font-semibold text-text break-all">{runtime?.commit || 'Unknown'}</dd>
        </div>
        <div class="rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm">
          <div class="mb-1.5 flex items-center gap-2 text-text-muted">
            <MdiIcon path={mdiCalendarClock} size={16} />
            <dt class="text-xs font-semibold uppercase tracking-wider">Build date</dt>
          </div>
          <dd class="font-mono text-sm font-semibold text-text break-all">{runtime?.build_date || 'Unknown'}</dd>
        </div>
      </dl>
    </section>

    <section class="rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="project-links">
      <h2 id="project-links" class="text-xl font-bold tracking-tight">Source and online inspector</h2>
      <p class="mt-1 text-sm text-text-muted">
        Inspect source code, report bugs, request features, or use the free hosted online inspector.
      </p>

      <nav class="mt-4 grid gap-3 sm:grid-cols-2" aria-label="Project links">
        <a
          class="inline-flex min-h-11 items-center justify-between gap-2 rounded-xl border border-border bg-surface-muted/30 p-3.5 text-sm font-medium text-text shadow-sm transition-colors hover:border-control/50 hover:bg-surface-muted"
          href="http://requestinspector.com"
          target="_blank"
          rel="noopener noreferrer"
        >
          <div class="flex items-center gap-2.5">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400">
              <MdiIcon path={mdiEarth} size={18} />
            </div>
            <span>RequestInspector.com (Free Online)</span>
          </div>
          <span class="text-text-muted"><MdiIcon path={mdiOpenInNew} size={14} /></span>
        </a>

        <a
          class="inline-flex min-h-11 items-center justify-between gap-2 rounded-xl border border-border bg-surface-muted/30 p-3.5 text-sm font-medium text-text shadow-sm transition-colors hover:border-control/50 hover:bg-surface-muted"
          href="https://github.com/logocomune/requestinspector-relay"
          target="_blank"
          rel="noopener noreferrer"
        >
          <div class="flex items-center gap-2.5">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-slate-500/10 text-slate-700 dark:text-slate-300">
              <MdiIcon path={mdiGithub} size={18} />
            </div>
            <span>GitHub repository</span>
          </div>
          <span class="text-text-muted"><MdiIcon path={mdiOpenInNew} size={14} /></span>
        </a>

        <a
          class="inline-flex min-h-11 items-center justify-between gap-2 rounded-xl border border-border bg-surface-muted/30 p-3.5 text-sm font-medium text-text shadow-sm transition-colors hover:border-control/50 hover:bg-surface-muted"
          href="https://github.com/logocomune/requestinspector-relay/issues"
          target="_blank"
          rel="noopener noreferrer"
        >
          <div class="flex items-center gap-2.5">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400">
              <MdiIcon path={mdiBugOutline} size={18} />
            </div>
            <span>GitHub issues</span>
          </div>
          <span class="text-text-muted"><MdiIcon path={mdiOpenInNew} size={14} /></span>
        </a>

        <a
          class="inline-flex min-h-11 items-center justify-between gap-2 rounded-xl border border-border bg-surface-muted/30 p-3.5 text-sm font-medium text-text shadow-sm transition-colors hover:border-control/50 hover:bg-surface-muted"
          href="/credits"
        >
          <div class="flex items-center gap-2.5">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-purple-500/10 text-purple-600 dark:text-purple-400">
              <MdiIcon path={mdiLicense} size={18} />
            </div>
            <span>Credits</span>
          </div>
          <span class="font-mono text-xs text-text-muted">Dependencies</span>
        </a>
      </nav>
    </section>

    <section class="rounded-xl border border-amber-500/30 bg-amber-500/5 p-5 shadow-sm" aria-labelledby="operator-notice">
      <div class="mb-2 flex items-center gap-2.5">
        <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400">
          <MdiIcon path={mdiShieldCheckOutline} size={20} />
        </div>
        <h2 id="operator-notice" class="text-lg font-bold tracking-tight">Operator responsibility</h2>
      </div>
      <p class="text-sm leading-relaxed text-text-muted">
        Captured traffic can contain credentials, personal data, or confidential application content. Operators are responsible for network exposure, authentication, retention, and export of captured data.
      </p>
    </section>

    <section class="rounded-xl border border-border bg-surface-muted/30 p-5 shadow-sm" aria-labelledby="disclaimer">
      <h2 id="disclaimer" class="mb-2 text-lg font-bold tracking-tight">Disclaimer</h2>
      <p class="text-sm leading-relaxed text-text-muted">
        This software is provided "as is", without warranty of any kind. The authors and contributors are not liable for direct, indirect, incidental, or consequential damage arising from its use.
      </p>
    </section>
  </article>
</main>
