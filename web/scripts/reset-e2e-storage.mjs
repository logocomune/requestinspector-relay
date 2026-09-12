import { rmSync } from 'node:fs';

for (const suffix of ['', '-shm', '-wal']) rmSync(`/tmp/reqrelay-phase10-e2e.db${suffix}`, { force: true });
