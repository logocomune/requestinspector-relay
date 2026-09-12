<script lang="ts">
  import SecondaryNavigation from './SecondaryNavigation.svelte';
  import ConnectionStatus from './ConnectionStatus.svelte';
</script>

<header class="border-b border-border bg-surface">
  <div class="mx-auto flex min-h-16 max-w-7xl flex-wrap items-center gap-2 px-3 py-2 sm:px-6">
    <a href="/" class="flex items-center gap-2 text-xl font-bold"><img src="/icons/icon-192.png" alt="" width="36" height="36" class="rounded-lg" /><span>RequestInspector Relay</span></a>
    <ConnectionStatus />
    <div class="ml-auto"><SecondaryNavigation /></div>
  </div>
</header>
