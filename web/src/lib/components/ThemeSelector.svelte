<script lang="ts">
  import { mdiThemeLightDark, mdiWeatherNight, mdiWhiteBalanceSunny } from '@mdi/js';
  import { onMount } from 'svelte';
  import { persistTheme, readTheme, watchSystemTheme, type ThemeChoice } from '$lib/theme';
  import MdiIcon from './MdiIcon.svelte';

  let choice = $state<ThemeChoice>('system');
  let stopWatching = () => {};

  onMount(() => {
    choice = readTheme();
    stopWatching = watchSystemTheme(() => choice);
    return stopWatching;
  });

  function select(next: ThemeChoice): void {
    choice = next;
    persistTheme(next);
  }

  const options = [
    { value: 'system' as const, label: 'System', icon: mdiThemeLightDark },
    { value: 'light' as const, label: 'Light', icon: mdiWhiteBalanceSunny },
    { value: 'dark' as const, label: 'Dark', icon: mdiWeatherNight }
  ];
</script>

<fieldset class="inline-flex rounded-xl border border-border bg-surface-muted/30 p-1 shadow-sm" aria-label="Color theme">
  <legend class="sr-only">Color theme</legend>
  {#each options as option}
    <button
      type="button"
      class="flex min-h-9 items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors"
      class:bg-action={choice === option.value}
      class:text-white={choice === option.value}
      class:shadow-sm={choice === option.value}
      class:text-text-muted={choice !== option.value}
      class:hover:text-text={choice !== option.value}
      aria-pressed={choice === option.value}
      title={`${option.label} theme`}
      onclick={() => select(option.value)}
    >
      <MdiIcon path={option.icon} size={16} />
      <span class="hidden xl:inline">{option.label}</span>
    </button>
  {/each}
</fieldset>
