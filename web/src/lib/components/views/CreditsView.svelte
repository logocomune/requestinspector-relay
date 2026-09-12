<script lang="ts">
  import {
    mdiArrowLeft,
    mdiLicense,
    mdiOpenInNew,
    mdiPaletteOutline,
    mdiServer,
    mdiWrench
  } from '@mdi/js';
  import { creditGroups } from '$lib/credits';
  import MdiIcon from '../MdiIcon.svelte';
  import PublicHeader from '../PublicHeader.svelte';

  function getGroupIcon(title: string): string {
    if (title.toLowerCase().includes('backend')) return mdiServer;
    if (title.toLowerCase().includes('frontend')) return mdiPaletteOutline;
    return mdiWrench;
  }
</script>

<PublicHeader />
<main class="mx-auto max-w-5xl p-3 sm:p-6">
  <div class="rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-8">
    <div class="mb-6 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
      <div class="flex items-center gap-3.5">
        <div class="feature-icon-wrap flex items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
          <MdiIcon path={mdiLicense} size={24} />
        </div>
        <div>
          <h1 class="text-2xl font-bold tracking-tight sm:text-3xl">Credits</h1>
          <p class="mt-0.5 text-sm text-text-muted">Libraries and tools powering RequestInspector Relay</p>
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
      Libraries and tools used to build RequestInspector Relay. Versions come from locked project manifests.
    </p>

    {#each creditGroups as group}
      <section class="mt-8 first:mt-4" aria-labelledby={`credit-${group.title.replaceAll(' ', '-').toLowerCase()}`}>
        <div class="mb-4 flex items-center gap-2.5">
          <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400">
            <MdiIcon path={getGroupIcon(group.title)} size={18} />
          </div>
          <h2 id={`credit-${group.title.replaceAll(' ', '-').toLowerCase()}`} class="text-xl font-bold tracking-tight">
            {group.title}
          </h2>
        </div>

        <div class="grid gap-3.5 md:grid-cols-2">
          {#each group.entries as entry}
            <article class="flex flex-col justify-between rounded-xl border border-border bg-surface-muted/30 p-4 shadow-sm transition-colors hover:bg-surface-muted/60">
              <div>
                <div class="flex items-start justify-between gap-2">
                  <h3 class="text-base font-semibold leading-snug">
                    <a
                      class="inline-flex items-center gap-1 text-link hover:underline"
                      href={entry.url}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      <span>{entry.name}</span>
                      <MdiIcon path={mdiOpenInNew} size={14} />
                    </a>
                  </h3>
                  <span class="inline-flex shrink-0 items-center rounded-md border border-border bg-surface px-2 py-0.5 font-mono text-xs font-medium text-text-muted">
                    {entry.version}
                  </span>
                </div>
                <p class="mt-2 text-sm leading-relaxed text-text-muted">{entry.purpose}</p>
              </div>
              <div class="mt-3 flex items-center justify-between border-t border-border/60 pt-3">
                <span class="text-xs text-text-muted">License</span>
                <span class="inline-flex items-center rounded-md border border-blue-500/20 bg-blue-500/10 px-2 py-0.5 font-mono text-xs font-medium text-blue-600 dark:text-blue-400">
                  {entry.license}
                </span>
              </div>
            </article>
          {/each}
        </div>
      </section>
    {/each}
  </div>
</main>
