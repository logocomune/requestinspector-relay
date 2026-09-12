export function formatDurationMilliseconds(microseconds: number | undefined): string {
  return microseconds === undefined ? '—' : `${(microseconds / 1000).toFixed(3)} ms`;
}
