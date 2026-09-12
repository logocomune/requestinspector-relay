import { readFileSync } from 'node:fs';

const packageLock = JSON.parse(readFileSync('package-lock.json', 'utf8'));
const goModule = readFileSync('../go.mod', 'utf8');
const goSum = readFileSync('../go.sum', 'utf8');
const source = readFileSync('src/lib/credits.ts', 'utf8');
const versionBlock = source.match(/export const lockedVersions = \{([\s\S]*?)\} as const;/)?.[1];

if (!versionBlock) throw new Error('credits.ts lockedVersions block is missing');

const credited = new Map([...versionBlock.matchAll(/^\s*'([^']+)': '([^']+)'/gm)].map((match) => [match[1], match[2]]));
const expected = new Map([['go', goModule.match(/^go\s+(\S+)/m)?.[1]]]);
const directGo = goModule.match(/require \(\n([\s\S]*?)\n\)/)?.[1] ?? '';
for (const match of directGo.matchAll(/^\s*(\S+)\s+(v\S+)/gm)) {
  expected.set(match[1], match[2]);
  if (!goSum.includes(`${match[1]} ${match[2]}`)) throw new Error(`${match[1]} ${match[2]} is absent from go.sum`);
}
const rootPackage = packageLock.packages[''];
for (const [name, version] of Object.entries({ ...rootPackage.dependencies, ...rootPackage.devDependencies })) expected.set(name, version);

const failures = [];
for (const [name, version] of expected) {
  if (credited.get(name) !== version) failures.push(`${name}: credit=${credited.get(name) ?? 'missing'} lock=${version}`);
  const accessor = /^[A-Za-z_$][\w$]*$/.test(name) ? `lockedVersions.${name}` : `lockedVersions['${name}']`;
  if (!source.slice(source.indexOf('export const creditGroups')).includes(accessor)) failures.push(`${name}: version not used by credit card`);
}
for (const name of credited.keys()) if (!expected.has(name)) failures.push(`${name}: credited version has no direct manifest entry`);

if (failures.length) throw new Error(`Credit metadata drift:\n${failures.join('\n')}`);
process.stdout.write(`Credit metadata matches ${expected.size} locked direct dependencies.\n`);
