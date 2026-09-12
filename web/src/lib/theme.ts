export const themeStorageKey = 'reqrelay.theme.v1';
export type ThemeChoice = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export function validTheme(value: unknown): value is ThemeChoice {
  return value === 'system' || value === 'light' || value === 'dark';
}

export function resolveTheme(choice: ThemeChoice, systemDark: boolean): ResolvedTheme {
  return choice === 'system' ? (systemDark ? 'dark' : 'light') : choice;
}

export function readTheme(storage: Pick<Storage, 'getItem'> = localStorage): ThemeChoice {
  const value = storage.getItem(themeStorageKey);
  return validTheme(value) ? value : 'system';
}

export function applyTheme(choice: ThemeChoice, media: MediaQueryList = matchMedia('(prefers-color-scheme: dark)')): void {
  document.documentElement.dataset.theme = resolveTheme(choice, media.matches);
}

export function persistTheme(choice: ThemeChoice): void {
  localStorage.setItem(themeStorageKey, choice);
  applyTheme(choice);
}

export function watchSystemTheme(choice: () => ThemeChoice): () => void {
  const media = matchMedia('(prefers-color-scheme: dark)');
  const update = () => { if (choice() === 'system') applyTheme('system', media); };
  media.addEventListener('change', update);
  return () => media.removeEventListener('change', update);
}
