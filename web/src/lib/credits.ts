export interface CreditEntry {
  name: string;
  version: string;
  url: string;
  license: string;
  purpose: string;
}

export interface CreditGroup {
  title: string;
  entries: CreditEntry[];
}

export const lockedVersions = {
  'go': '1.26.0',
  'github.com/ardanlabs/conf/v3': 'v3.13.0',
	'github.com/hashicorp/golang-lru/v2': 'v2.0.7',
  'golang.org/x/sys': 'v0.48.0',
  'gopkg.in/yaml.v3': 'v3.0.1',
  'modernc.org/sqlite': 'v1.58.0',
  '@mdi/js': '7.4.47',
  'svelte': '5.57.0',
  '@playwright/test': '1.63.0',
  '@sveltejs/adapter-static': '3.0.10',
  '@sveltejs/kit': '2.70.3',
  '@tailwindcss/vite': '4.3.3',
  '@types/node': '26.4.1',
  'fast-check': '4.9.0',
  'svelte-check': '4.7.6',
  'tailwindcss': '4.3.3',
  'typescript': '6.0.3',
  'vite': '8.2.2',
  'vitest': '5.0.0'
} as const;

export const creditGroups: CreditGroup[] = [
  { title: 'Go backend libraries', entries: [
    { name: 'Go and standard library', version: lockedVersions.go, url: 'https://go.dev/', license: 'BSD-3-Clause', purpose: 'Application runtime, networking, HTTP, concurrency, and tooling.' },
    { name: 'Ardan Labs conf', version: lockedVersions['github.com/ardanlabs/conf/v3'], url: 'https://github.com/ardanlabs/conf', license: 'MIT', purpose: 'Configuration defaults, environment variables, flags, and YAML loading.' },
		{ name: 'HashiCorp golang-lru', version: lockedVersions['github.com/hashicorp/golang-lru/v2'], url: 'https://github.com/hashicorp/golang-lru', license: 'MPL-2.0', purpose: 'Bounded expiring UDP proxy session mappings.' },
    { name: 'Go system calls', version: lockedVersions['golang.org/x/sys'], url: 'https://pkg.go.dev/golang.org/x/sys', license: 'BSD-3-Clause', purpose: 'Portable low-level operating-system integration.' },
    { name: 'go-yaml', version: lockedVersions['gopkg.in/yaml.v3'], url: 'https://github.com/go-yaml/yaml', license: 'MIT', purpose: 'YAML configuration parsing and persisted UI overrides.' },
    { name: 'modernc SQLite', version: lockedVersions['modernc.org/sqlite'], url: 'https://pkg.go.dev/modernc.org/sqlite', license: 'BSD-3-Clause', purpose: 'CGO-free durable request history.' }
  ] },
  { title: 'Frontend libraries', entries: [
    { name: 'Svelte', version: lockedVersions.svelte, url: 'https://svelte.dev/', license: 'MIT', purpose: 'Reactive user interface.' },
    { name: 'SvelteKit', version: lockedVersions['@sveltejs/kit'], url: 'https://svelte.dev/docs/kit', license: 'MIT', purpose: 'Static application routing and build structure.' },
    { name: 'Tailwind CSS', version: lockedVersions.tailwindcss, url: 'https://tailwindcss.com/', license: 'MIT', purpose: 'Compiled responsive design utilities.' },
    { name: 'Material Design Icons', version: lockedVersions['@mdi/js'], url: 'https://pictogrammers.com/library/mdi/', license: 'Apache-2.0', purpose: 'Interface icons shipped with application.' }
  ] },
  { title: 'Development and testing tools', entries: [
    { name: 'Vite', version: lockedVersions.vite, url: 'https://vite.dev/', license: 'MIT', purpose: 'Frontend build tooling.' },
    { name: 'TypeScript', version: lockedVersions.typescript, url: 'https://www.typescriptlang.org/', license: 'Apache-2.0', purpose: 'Static frontend type checking.' },
    { name: 'Vitest', version: lockedVersions.vitest, url: 'https://vitest.dev/', license: 'MIT', purpose: 'Frontend unit tests.' },
    { name: 'fast-check', version: lockedVersions['fast-check'], url: 'https://fast-check.dev/', license: 'MIT', purpose: 'Property-based frontend tests.' },
    { name: 'Playwright', version: lockedVersions['@playwright/test'], url: 'https://playwright.dev/', license: 'Apache-2.0', purpose: 'Browser end-to-end and accessibility journeys.' },
    { name: 'Svelte adapter-static', version: lockedVersions['@sveltejs/adapter-static'], url: 'https://svelte.dev/docs/kit/adapter-static', license: 'MIT', purpose: 'Static embedded application output.' },
    { name: 'Tailwind Vite plugin', version: lockedVersions['@tailwindcss/vite'], url: 'https://tailwindcss.com/docs/installation/using-vite', license: 'MIT', purpose: 'Tailwind build integration.' },
    { name: 'Node.js type definitions', version: lockedVersions['@types/node'], url: 'https://github.com/DefinitelyTyped/DefinitelyTyped', license: 'MIT', purpose: 'Build-script type declarations.' },
    { name: 'svelte-check', version: lockedVersions['svelte-check'], url: 'https://github.com/sveltejs/language-tools', license: 'MIT', purpose: 'Svelte diagnostics and accessibility checks.' }
  ] }
];
