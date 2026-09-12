export function serviceWorkerShellAssets(
  build: string[],
  files: string[],
  prerendered: string[],
  development: boolean
): string[] {
  const navigation = development ? ['/'] : ['/', '/index.html'];
  return [...new Set([...build, ...files, ...prerendered, ...navigation])];
}
