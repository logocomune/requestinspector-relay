<script lang="ts">
  import {
    mdiArrowLeft,
    mdiCameraOutline,
    mdiHelpCircleOutline,
    mdiInformationOutline,
    mdiSwapHorizontalBold
  } from '@mdi/js';
  import MdiIcon from '../MdiIcon.svelte';
  import PublicHeader from '../PublicHeader.svelte';

  export type HelpTopic = 'overview' | 'http-capture' | 'udp-capture' | 'http-proxy' | 'udp-proxy';
  interface Props { topic: HelpTopic; }
  let { topic }: Props = $props();

  const topics: Record<HelpTopic, { title: string; eyebrow: string }> = {
    overview: { title: 'How RequestInspector Relay works', eyebrow: 'Overview' },
    'http-capture': { title: 'HTTP Capture', eyebrow: 'HTTP' },
    'udp-capture': { title: 'UDP Capture', eyebrow: 'UDP' },
    'http-proxy': { title: 'HTTP Proxy', eyebrow: 'HTTP' },
    'udp-proxy': { title: 'UDP Proxy', eyebrow: 'UDP' }
  };
  let current = $derived(topics[topic]);
</script>

<PublicHeader />
<main class="mx-auto max-w-5xl p-3 sm:p-6">
  <div class="rounded-2xl border border-border bg-surface p-5 shadow-sm sm:p-8">
    <div class="mb-6 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
      <div class="flex items-center gap-3.5">
        <div class="feature-icon-wrap flex items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
          <MdiIcon path={mdiHelpCircleOutline} size={24} />
        </div>
        <div>
          <p class="text-xs font-semibold uppercase tracking-wider text-text-muted">Help · {current.eyebrow}</p>
          <h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{current.title}</h1>
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

    <nav class="flex gap-2 overflow-x-auto border-b border-border pb-3" aria-label="Help topics">
      <a
        href="/help"
        class="shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold transition-colors {topic === 'overview'
          ? 'border border-blue-500/20 bg-blue-500/10 text-blue-600 dark:text-blue-400'
          : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
      >
        Overview
      </a>
      <a
        href="/help/http-capture"
        class="shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold transition-colors {topic === 'http-capture'
          ? 'border border-blue-500/20 bg-blue-500/10 text-blue-600 dark:text-blue-400'
          : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
      >
        HTTP Capture
      </a>
      <a
        href="/help/udp-capture"
        class="shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold transition-colors {topic === 'udp-capture'
          ? 'border border-blue-500/20 bg-blue-500/10 text-blue-600 dark:text-blue-400'
          : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
      >
        UDP Capture
      </a>
      <a
        href="/help/http-proxy"
        class="shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold transition-colors {topic === 'http-proxy'
          ? 'border border-blue-500/20 bg-blue-500/10 text-blue-600 dark:text-blue-400'
          : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
      >
        HTTP Proxy
      </a>
      <a
        href="/help/udp-proxy"
        class="shrink-0 rounded-lg px-3.5 py-2 text-sm font-semibold transition-colors {topic === 'udp-proxy'
          ? 'border border-blue-500/20 bg-blue-500/10 text-blue-600 dark:text-blue-400'
          : 'text-text-muted hover:bg-surface-muted hover:text-text'}"
      >
        UDP Proxy
      </a>
    </nav>

    {#if topic === 'overview'}
      <section class="mt-6 space-y-6">
        <p class="text-base text-text-muted sm:text-lg">
          RequestInspector Relay receives HTTP and UDP traffic, records it in Realtime and History, then applies an independent Capture or Proxy mode to each transport.
        </p>

        <div class="grid gap-4 md:grid-cols-2">
          <article class="rounded-xl border border-border bg-surface-muted/30 p-5 shadow-sm transition-colors hover:bg-surface-muted/60">
            <div class="mb-3 flex items-center gap-2.5">
              <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400">
                <MdiIcon path={mdiCameraOutline} size={20} />
              </div>
              <h2 class="text-xl font-bold tracking-tight">Capture</h2>
            </div>
            <div class="mt-3 flex flex-wrap items-center gap-2 font-mono text-sm">
              <span class="rounded-md border border-border bg-surface px-2.5 py-1 text-xs">Client</span>
              <span aria-hidden="true" class="text-text-muted">→</span>
              <span class="rounded-md border border-blue-500/20 bg-blue-500/10 px-2.5 py-1 text-xs font-semibold text-blue-600 dark:text-blue-400">RequestInspector Relay</span>
              <span aria-hidden="true" class="text-text-muted">→</span>
              <span class="rounded-md border border-border bg-surface px-2.5 py-1 text-xs">Configured reply</span>
            </div>
            <p class="mt-4 text-sm leading-relaxed text-text-muted">
              No upstream is contacted. Use it to inspect clients, reproduce a fixed response, or debug a sender.
            </p>
          </article>

          <article class="rounded-xl border border-border bg-surface-muted/30 p-5 shadow-sm transition-colors hover:bg-surface-muted/60">
            <div class="mb-3 flex items-center gap-2.5">
              <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                <MdiIcon path={mdiSwapHorizontalBold} size={20} />
              </div>
              <h2 class="text-xl font-bold tracking-tight">Proxy</h2>
            </div>
            <div class="mt-3 flex flex-wrap items-center gap-2 font-mono text-sm">
              <span class="rounded-md border border-border bg-surface px-2.5 py-1 text-xs">Client</span>
              <span aria-hidden="true" class="text-text-muted">→</span>
              <span class="rounded-md border border-blue-500/20 bg-blue-500/10 px-2.5 py-1 text-xs font-semibold text-blue-600 dark:text-blue-400">RequestInspector Relay</span>
              <span aria-hidden="true" class="text-text-muted">→</span>
              <span class="rounded-md border border-border bg-surface px-2.5 py-1 text-xs">Configured upstream</span>
            </div>
            <p class="mt-4 text-sm leading-relaxed text-text-muted">
              Traffic is recorded while RequestInspector Relay forwards it to one configured upstream.
            </p>
          </article>
        </div>

        <div class="rounded-xl border border-blue-500/20 bg-blue-500/5 p-4 text-sm text-text-muted shadow-sm">
          <div class="mb-2 flex items-center gap-2 font-semibold text-text">
            <MdiIcon path={mdiInformationOutline} size={18} />
            <span>Architecture Notes</span>
          </div>
          <div class="space-y-2 leading-relaxed">
            <p><strong>Choose transport first:</strong> use the HTTP/UDP control on Realtime and History to inspect each protocol independently.</p>
            <p><strong>Apply settings safely:</strong> HTTP and UDP listener settings hot reload their ingest plane independently. Existing UDP proxy sessions close when UDP settings change.</p>
            <p><strong>Find traffic:</strong> Realtime shows new exchanges; History retains completed exchanges and offers full payload inspection where available.</p>
          </div>
        </div>
      </section>
    {:else if topic === 'http-capture'}
      <section class="mt-6 space-y-6">
        <p class="text-base text-text-muted sm:text-lg">
          HTTP Capture records method, path, headers, and body, then returns one configured HTTP response. It never contacts an upstream.
        </p>
        <div class="rounded-xl border border-border bg-surface-muted/40 p-4 font-mono text-sm shadow-sm">
          Client HTTP request → RequestInspector Relay → configured HTTP status, headers, and body
        </div>
        <ol class="list-decimal space-y-2 pl-6 text-sm text-text-muted leading-relaxed">
          <li>Open <a class="text-link underline" href="/settings">Settings</a> → <strong class="text-text">HTTP</strong>.</li>
          <li>Set Operating mode to Capture, then configure status, headers, and body under <strong class="text-text">HTTP Capture</strong>.</li>
          <li>Save. New HTTP requests use the response immediately; active requests keep their existing mode.</li>
        </ol>
        <div class="rounded-xl border border-blue-500/20 bg-blue-500/5 p-4 text-sm text-text-muted shadow-sm">
          <strong>Use when:</strong> you need to observe client behavior, return a deterministic fixture, or diagnose a sender without calling a real service.
        </div>
        <div class="terminal-block">
          <pre class="m-0 text-sm"><code>mode: capture
capture_response:
  status: 200
  body: '&#123;"ok":true&#125;'</code></pre>
        </div>
      </section>
    {:else if topic === 'udp-capture'}
      <section class="mt-6 space-y-6">
        <p class="text-base text-text-muted sm:text-lg">
          UDP Capture records each datagram with source, listener, byte count, and payload. By default it sends no reply.
        </p>
        <div class="rounded-xl border border-border bg-surface-muted/40 p-4 font-mono text-sm shadow-sm">
          UDP client datagram → RequestInspector Relay → History / Realtime<br />
          <span class="text-text-muted">optional configured reply → same UDP client</span>
        </div>
        <ol class="list-decimal space-y-2 pl-6 text-sm text-text-muted leading-relaxed">
          <li>Open <a class="text-link underline" href="/settings">Settings</a> → <strong class="text-text">UDP</strong>.</li>
          <li>Enable UDP, choose Capture mode, listener, and maximum datagram size.</li>
          <li>Under <strong class="text-text">UDP Capture</strong>, optionally enter a reply; empty means receive and record only.</li>
          <li>Save, confirm the reload toast and active UDP listener, then send test traffic.</li>
        </ol>
        <div class="rounded-xl border border-amber-500/20 bg-amber-500/5 p-4 text-sm text-text-muted shadow-sm">
          <strong>Reply limit:</strong> a configured reply never exceeds the accepted datagram size. Oversized datagrams are recorded as discarded.
        </div>
        <div class="terminal-block">
          <pre class="m-0 text-sm"><code>udp:
  enabled: true
  listen: 127.0.0.1:9000
  mode: capture
  capture_response: "ok"</code></pre>
        </div>
      </section>
    {:else if topic === 'http-proxy'}
      <section class="mt-6 space-y-6">
        <p class="text-base text-text-muted sm:text-lg">
          HTTP Proxy records a request, forwards it to one configured upstream URL, then records and returns the upstream response.
        </p>
        <div class="rounded-xl border border-border bg-surface-muted/40 p-4 font-mono text-sm shadow-sm">
          Client HTTP request → RequestInspector Relay → upstream HTTP service → RequestInspector Relay → client
        </div>
        <ol class="list-decimal space-y-2 pl-6 text-sm text-text-muted leading-relaxed">
          <li>Open <a class="text-link underline" href="/settings">Settings</a> → <strong class="text-text">HTTP</strong> → <strong class="text-text">HTTP Proxy</strong>.</li>
          <li>Set upstream URL, timeout, response limits, and concurrent-exchange capacity.</li>
          <li>Set Operating mode to Proxy, then save. New HTTP requests switch immediately.</li>
        </ol>
        <div class="rounded-xl border border-blue-500/20 bg-blue-500/5 p-4 text-sm text-text-muted shadow-sm">
          <strong>Before switching:</strong> Proxy requires a valid upstream URL. Existing in-flight exchanges retain the mode chosen when they started.
        </div>
        <div class="terminal-block">
          <pre class="m-0 text-sm"><code>mode: proxy
upstream:
  url: https://api.example.test/service
  timeout: 30s</code></pre>
        </div>
      </section>
    {:else}
      <section class="mt-6 space-y-6">
        <p class="text-base text-text-muted sm:text-lg">
          UDP Proxy records a datagram, forwards it only to one configured unicast upstream, and maps permitted replies back to the originating active client.
        </p>
        <div class="rounded-xl border border-border bg-surface-muted/40 p-4 font-mono text-sm shadow-sm">
          UDP client A → RequestInspector Relay → fixed UDP upstream<br />
          UDP upstream reply → RequestInspector Relay → UDP client A
        </div>
        <ol class="list-decimal space-y-2 pl-6 text-sm text-text-muted leading-relaxed">
          <li>Open <a class="text-link underline" href="/settings">Settings</a> → <strong class="text-text">UDP</strong>, enable UDP, and set Mode to Proxy.</li>
          <li>Under <strong class="text-text">UDP Proxy</strong>, set one unicast upstream, session capacity, TTL, and reply-rate limits.</li>
          <li>Save, confirm the reload toast and active UDP listener, then send a test datagram.</li>
        </ol>
        <div class="rounded-xl border border-amber-500/20 bg-amber-500/5 p-4 text-sm text-text-muted shadow-sm">
          <strong>Security boundary:</strong> broadcast, multicast, payload-selected destinations, and replies after session expiry are rejected. Mappings expire after TTL and capacity is bounded. Changing UDP settings closes existing mappings.
        </div>
        <div class="terminal-block">
          <pre class="m-0 text-sm"><code>udp:
  enabled: true
  mode: proxy
  upstream: 127.0.0.1:9001
  session_ttl: 30s</code></pre>
        </div>
      </section>
    {/if}
  </div>
</main>
