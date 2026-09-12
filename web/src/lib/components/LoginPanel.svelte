<script lang="ts">
  import { mdiShieldLockOutline } from '@mdi/js';
  import MdiIcon from './MdiIcon.svelte';

  interface Props { busy: boolean; error: string; onlogin: (username: string, password: string) => void }
  let { busy, error, onlogin }: Props = $props();
  let username = $state('');
  let password = $state('');
</script>

<main class="mx-auto flex min-h-[75vh] max-w-lg items-center px-4 py-8">
  <div class="w-full rounded-2xl border border-border bg-surface p-6 shadow-sm sm:p-8">
    <div class="mb-5 flex items-center gap-3.5">
      <div class="feature-icon-wrap flex items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
        <MdiIcon path={mdiShieldLockOutline} size={24} />
      </div>
      <div>
        <h1 class="text-xl font-bold tracking-tight sm:text-2xl">Sign In</h1>
        <p class="text-xs text-text-muted sm:text-sm">Management access requires authentication</p>
      </div>
    </div>

    <p class="mb-6 text-sm leading-relaxed text-text-muted">
      Sign in with your configured management credentials to manage listeners, routes, and persistence settings.
    </p>

    {#if error}
      <div role="alert" class="mb-4 rounded-xl border border-red-500/30 bg-red-500/10 p-3 text-sm font-medium text-red-600 dark:text-red-400 shadow-sm">
        {error}
      </div>
    {/if}

    <form onsubmit={(event) => { event.preventDefault(); onlogin(username, password); }}>
      <div class="mb-4">
        <label class="mb-1.5 block text-sm font-semibold" for="username">Username</label>
        <input
          id="username"
          name="username"
          autocomplete="username"
          required
          bind:value={username}
          class="min-h-11 w-full rounded-xl border border-border bg-surface-muted/30 px-3.5 text-sm transition-colors focus:border-blue-500 focus:bg-surface focus:outline-none"
          placeholder="admin"
        />
      </div>

      <div class="mb-6">
        <label class="mb-1.5 block text-sm font-semibold" for="password">Password</label>
        <input
          id="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
          bind:value={password}
          class="min-h-11 w-full rounded-xl border border-border bg-surface-muted/30 px-3.5 text-sm transition-colors focus:border-blue-500 focus:bg-surface focus:outline-none"
        />
      </div>

      <button
        type="submit"
        disabled={busy}
        class="min-h-11 w-full rounded-xl bg-action px-4 font-semibold text-white shadow-sm transition-colors hover:bg-action/90 disabled:opacity-50"
      >
        {busy ? 'Signing in…' : 'Sign in'}
      </button>
    </form>
  </div>
</main>
