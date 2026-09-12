<script lang="ts">
  import { mdiClose } from '@mdi/js';
  import { fly } from 'svelte/transition';
  import MdiIcon from './MdiIcon.svelte';

  export interface ToastMessage { id: number; message: string; tone?: 'action' | 'error' }
  interface Props { messages: ToastMessage[]; onclose: (id: number) => void }
  let { messages, onclose }: Props = $props();
</script>

<div class="pointer-events-none fixed right-4 top-4 z-50 flex max-w-sm flex-col items-end gap-2">
  {#each messages as toast (toast.id)}
    <div in:fly={{ y: -20, duration: 200 }} out:fly={{ y: -20, duration: 180 }} class={`pointer-events-auto flex w-full items-center gap-3 rounded-xl border p-3 shadow-xl backdrop-blur ${toast.tone === 'error' ? 'border-error/70 bg-error/40 text-text' : 'border-action/70 bg-action/40 text-white'}`} role="status" aria-live="polite">
      <span class="flex-1">{toast.message}</span>
      <button type="button" class="flex size-8 shrink-0 items-center justify-center rounded text-current hover:bg-white/20" aria-label="Dismiss notification" title="Dismiss notification" onclick={() => onclose(toast.id)}><MdiIcon path={mdiClose} size={18} /></button>
    </div>
  {/each}
</div>
