import { cpSync, mkdirSync, readdirSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const runtime = fileURLToPath(new URL('../', import.meta.url))
const source = join(runtime, 'dist/harness-dsh/src')
mkdirSync(source, { recursive: true })
cpSync(join(runtime, '../harness/dsh/src'), source, { recursive: true })
// Compile against the runtime dependencies, as the Docker builder does.
const result = spawnSync(process.execPath, [
  join(runtime, 'node_modules/typescript/bin/tsc'), '--ignoreConfig',
  '--target', 'ES2022', '--module', 'NodeNext', '--moduleResolution', 'NodeNext',
  '--strict', '--declaration', '--verbatimModuleSyntax', '--noUncheckedIndexedAccess',
  '--exactOptionalPropertyTypes', '--skipLibCheck',
  '--outDir', join(runtime, 'dist/harness-dsh/lib'),
  ...readdirSync(source).filter(name => name.endsWith('.ts')).map(name => join(source, name)),
], { cwd: runtime, stdio: 'inherit' })
if (result.error) throw result.error
process.exitCode = result.status ?? 1
