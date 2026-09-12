<script lang="ts">
  import { onMount } from 'svelte';

  interface Props { live?: boolean }
  let { live }: Props = $props();
  let observedLive = $state(false);
  let isLive = $derived(live ?? observedLive);
  let label = $derived(isLive ? 'Live' : 'Offline');

  onMount(() => {
    if (live !== undefined) return;
    const source = new EventSource('/api/v1/events');
    const online = () => { observedLive = false; };
    const offline = () => { observedLive = false; };
    source.onopen = () => { observedLive = true; };
    source.onerror = () => { observedLive = false; };
    window.addEventListener('online', online);
    window.addEventListener('offline', offline);
    return () => {
      source.close();
      window.removeEventListener('online', online);
      window.removeEventListener('offline', offline);
    };
  });
</script>

<span
  class={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold shadow-sm ${
    isLive
      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
      : 'border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400'
  }`}
  role="status"
  aria-live="polite"
  title={label}
>
  <span class={`size-2 rounded-full ${isLive ? 'bg-emerald-500' : 'bg-red-500 animate-pulse'}`} aria-hidden="true"></span>
  <span>{label}</span>
</span>
