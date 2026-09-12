<script lang="ts">
  import type { OperatingMode } from '$lib/api/types';
  interface Props { mode: OperatingMode; busy: boolean; onchange: (mode: OperatingMode) => void }
  let { mode, busy, onchange }: Props = $props();
</script>

<fieldset class="flex rounded-md border border-control" disabled={busy} aria-label="Operating mode">
  <legend class="sr-only">Operating mode</legend>
  {#each ['capture', 'proxy'] as option}
    <button
      type="button"
      class="min-h-11 px-3 text-sm font-semibold capitalize disabled:opacity-60"
      class:bg-action={mode === option}
      class:text-white={mode === option}
      aria-pressed={mode === option}
      onclick={() => onchange(option as OperatingMode)}
    >{option}</button>
  {/each}
</fieldset>
