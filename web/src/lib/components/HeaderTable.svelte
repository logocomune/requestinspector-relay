<script lang="ts">
  import { mdiBinoculars } from '@mdi/js';
  import { inspectUserAgent, userAgentParserURL } from '$lib/user-agent';
  import CopyButton from './CopyButton.svelte';
  import MdiIcon from './MdiIcon.svelte';
  interface Props { headers: Record<string, string[]> }
  let { headers }: Props = $props();
  let userAgent = $state<string>();
  let block = $derived(Object.entries(headers).flatMap(([name, values]) => values.map((value) => `${name}: ${value}`)).join('\r\n'));
</script>

<div class="mt-4 flex items-center justify-between gap-3">
  <h3 class="font-semibold">Headers</h3>
  <div class="flex items-center gap-2">
    <CopyButton value={block} label="Copy header block" />
  </div>
</div>
<dl class="mt-2 divide-y divide-border rounded border border-border">
  {#each Object.entries(headers) as [name, values]}
    {#each values as value}
      <div class="grid items-center gap-1 p-2 sm:grid-cols-[minmax(8rem,12rem)_1fr]">
        <dt class="flex items-center font-semibold break-all">{name}:</dt>
        <dd class="flex min-w-0 items-center gap-2 break-words">
          <span class="min-w-0 break-words">{value}</span>
          {#if name.toLowerCase() === 'user-agent'}
            <a href={userAgentParserURL(value)} target="_blank" rel="noopener noreferrer" class="inline-flex min-h-10 min-w-10 shrink-0 items-center justify-center rounded bg-action text-white" onclick={() => { userAgent = value; }} aria-label="Open User-Agent parser" title="Open User-Agent parser in a new tab">
              <MdiIcon path={mdiBinoculars} size={18} />
            </a>
          {/if}
        </dd>
      </div>
    {/each}
  {/each}
</dl>
{#if userAgent}
  {@const details = inspectUserAgent(userAgent)}
  <section class="mt-2 rounded border border-border bg-surface-muted p-3" aria-label="User-Agent details">
    <h4 class="font-semibold">User-Agent inspection</h4>
    <p class="mt-1 break-all font-mono text-sm">{userAgent}</p>
    <dl class="mt-2 grid grid-cols-[max-content_1fr] gap-x-3 text-sm">
      <dt>Browser</dt><dd>{details.browser}</dd><dt>Engine</dt><dd>{details.engine}</dd>
      <dt>Operating system</dt><dd>{details.operatingSystem}</dd><dt>Device</dt><dd>{details.device}</dd>
    </dl>
  </section>
{/if}
