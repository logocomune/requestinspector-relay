<script lang="ts">
  interface Props { method: string }
  let { method }: Props = $props();
  let color = $derived(
    method === 'POST'
      ? 'bg-emerald-600'
      : method === 'GET'
      ? 'bg-blue-600'
      : method === 'DELETE'
      ? 'bg-rose-600'
      : method === 'PUT' || method === 'PATCH'
      ? 'bg-amber-600'
      : 'bg-slate-600'
  );
</script>

<span class={`inline-flex min-w-12 items-center justify-center rounded-md px-2 py-0.5 font-mono text-xs font-bold uppercase tracking-wider text-white shadow-sm ${color}`}>{method}</span>
