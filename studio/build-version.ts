import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'

export function normalizeCommit(value: string | undefined): string | undefined {
  const trimmed = value?.trim()
  return trimmed && /^[a-f0-9]{40}$/i.test(trimmed) ? trimmed.toLowerCase() : undefined
}

// Only a full, validated SHA is exported. Never expose arbitrary CI environment values.
export function resolveBuildCommit(root: string, build: boolean): string {
  let commit = normalizeCommit(process.env.STUDIO_COMMIT_SHA)
  if (!commit) {
    try { commit = normalizeCommit(execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] })) } catch { /* Docker has no Git metadata. */ }
  }
  const marker = path.join(root, '.build-commit')
  if (!commit && existsSync(marker)) commit = normalizeCommit(readFileSync(marker, 'utf8'))
  if (!commit && process.env.STUDIO_REQUIRE_BUILD_COMMIT === '1') {
    throw new Error('Missing Studio commit. Build Studio in the checkout first, or pass --build-arg STUDIO_COMMIT_SHA=<full git SHA>.')
  }
  // The host build hands this small file to the Docker build, without copying .git.
  if (commit && build) writeFileSync(marker, commit + '\n')
  return commit ?? 'unknown'
}
