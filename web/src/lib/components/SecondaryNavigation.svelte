<script lang="ts">
  import { page } from '$app/state';
  import { mdiCogOutline, mdiDotsVertical, mdiHelpCircleOutline, mdiInformationOutline, mdiStarOutline } from '@mdi/js';
  import MdiIcon from './MdiIcon.svelte';

  let pathname = $derived(page.url.pathname);
  let isSettings = $derived(pathname.startsWith('/settings'));
  let isHelp = $derived(pathname.startsWith('/help'));
  let isAbout = $derived(pathname.startsWith('/about'));
  let isCredits = $derived(pathname.startsWith('/credits'));
</script>

<nav class="flex items-center gap-1" aria-label="Secondary">
  <a
    href="/settings"
    class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isSettings ? 'border border-border bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
    aria-current={isSettings ? 'page' : undefined}
  >
    <MdiIcon path={mdiCogOutline} size={20} />
    <span class="hidden lg:inline">Settings</span>
    <span class="sr-only lg:hidden">Settings</span>
  </a>
  <div class="hidden lg:flex lg:items-center lg:gap-1">
    <details class="relative">
      <summary
        class="flex min-h-11 cursor-pointer list-none items-center gap-2 rounded px-3 font-semibold transition-colors {isHelp ? 'border border-border bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
        aria-label="Help topics"
      >
        <MdiIcon path={mdiHelpCircleOutline} size={20} /> Help
      </summary>
      <div class="absolute right-0 z-20 mt-1 min-w-52 rounded border border-border bg-surface p-1 shadow-lg">
        <a href="/help" class="flex min-h-11 items-center rounded px-3 font-semibold {pathname === '/help' ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}">Overview</a>
        <a href="/help/http-capture" class="flex min-h-11 items-center rounded px-3 font-semibold {pathname === '/help/http-capture' ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}">HTTP Capture</a>
        <a href="/help/udp-capture" class="flex min-h-11 items-center rounded px-3 font-semibold {pathname === '/help/udp-capture' ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}">UDP Capture</a>
        <a href="/help/http-proxy" class="flex min-h-11 items-center rounded px-3 font-semibold {pathname === '/help/http-proxy' ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}">HTTP Proxy</a>
        <a href="/help/udp-proxy" class="flex min-h-11 items-center rounded px-3 font-semibold {pathname === '/help/udp-proxy' ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}">UDP Proxy</a>
      </div>
    </details>
    <a
      href="/about"
      class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isAbout ? 'border border-border bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
      aria-current={isAbout ? 'page' : undefined}
    >
      <MdiIcon path={mdiInformationOutline} size={20} /> About
    </a>
    <a
      href="/credits"
      class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isCredits ? 'border border-border bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
      aria-current={isCredits ? 'page' : undefined}
    >
      <MdiIcon path={mdiStarOutline} size={20} /> Credits
    </a>
  </div>
  <details class="relative lg:hidden">
    <summary
      class="flex min-h-11 min-w-11 cursor-pointer list-none items-center justify-center rounded text-link hover:bg-surface-muted"
      class:text-text={isAbout || isCredits || isHelp}
      class:bg-surface-muted={isAbout || isCredits || isHelp}
      aria-label="More pages"
    >
      <MdiIcon path={mdiDotsVertical} size={22} />
      <span class="sr-only">More pages</span>
    </summary>
    <div class="absolute right-0 z-20 mt-1 min-w-40 rounded border border-border bg-surface p-1 shadow-lg">
      <a
        href="/help"
        class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isHelp ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
        aria-current={isHelp ? 'page' : undefined}
      >
        <MdiIcon path={mdiHelpCircleOutline} size={20} /> Help
      </a>
      <a
        href="/about"
        class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isAbout ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
        aria-current={isAbout ? 'page' : undefined}
      >
        <MdiIcon path={mdiInformationOutline} size={20} /> About
      </a>
      <a
        href="/credits"
        class="flex min-h-11 items-center gap-2 rounded px-3 font-semibold transition-colors {isCredits ? 'bg-surface-muted text-text' : 'text-link hover:bg-surface-muted'}"
        aria-current={isCredits ? 'page' : undefined}
      >
        <MdiIcon path={mdiStarOutline} size={20} /> Credits
      </a>
    </div>
  </details>
</nav>
