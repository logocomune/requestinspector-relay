<script lang="ts">
  import { mdiCheck, mdiContentCopy } from '@mdi/js';
  import MdiIcon from './MdiIcon.svelte';
  interface Props { value: string; label: string }
  let { value, label }: Props = $props();
  let feedback = $state('');

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      feedback = 'Copied';
    } catch {
      feedback = 'Copy failed';
    }
    window.setTimeout(() => { feedback = ''; }, 1500);
  }
</script>

<div class="inline-flex items-center gap-1.5">
  <button
    type="button"
    class="inline-flex min-h-8 min-w-8 items-center justify-center rounded-lg border border-border bg-surface px-2 text-text-muted transition-colors hover:bg-surface-muted hover:text-text shadow-sm"
    onclick={copy}
    title={label}
    aria-label={label}
  >
    <MdiIcon path={feedback === 'Copied' ? mdiCheck : mdiContentCopy} size={16} />
  </button>
  {#if feedback}
    <span class="text-xs font-medium text-emerald-600 dark:text-emerald-400" role="status" aria-live="polite">{feedback}</span>
  {/if}
</div>
