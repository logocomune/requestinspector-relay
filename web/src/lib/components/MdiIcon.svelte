<script lang="ts">
  interface Props { path: string; size?: number; title?: string }
  let { path, size = 24, title }: Props = $props();
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="currentColor"
  role={title ? 'img' : undefined}
  aria-label={title}
  aria-hidden={title ? undefined : 'true'}
  focusable="false"
>
  <path d={path} />
</svg>
