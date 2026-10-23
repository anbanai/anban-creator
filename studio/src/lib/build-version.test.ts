
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { normalizeCommit, resolveBuildCommit } from '../../build-version'
const sha = 'a'.repeat(40)
const dirs: string[] = []
function root() { const dir = mkdtempSync(path.join(tmpdir(), 'studio-version-')); dirs.push(dir); return dir }
afterEach(() => { vi.unstubAllEnvs(); for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true }) })
describe('build revision handoff', () => {
  it('accepts only full commit IDs, never arbitrary environment values', () => {
    expect(normalizeCommit('private-token')).toBeUndefined()
    expect(normalizeCommit('1234567')).toBeUndefined()
    expect(normalizeCommit(' A'.trim().repeat(40))).toBe(sha)
  })
  it('writes the explicit revision and reads it back in a source-only Docker context', () => {
    const dir = root()
    vi.stubEnv('STUDIO_COMMIT_SHA', sha)
    expect(resolveBuildCommit(dir, true)).toBe(sha)
    expect(readFileSync(path.join(dir, '.build-commit'), 'utf8').trim()).toBe(sha)
    vi.stubEnv('STUDIO_COMMIT_SHA', '')
    vi.stubEnv('STUDIO_REQUIRE_BUILD_COMMIT', '1')
    expect(resolveBuildCommit(dir, true)).toBe(sha)
  })
  it('fails a production build rather than publishing an unidentified version', () => {
    const dir = root()
    vi.stubEnv('STUDIO_COMMIT_SHA', '')
    vi.stubEnv('STUDIO_REQUIRE_BUILD_COMMIT', '1')
    writeFileSync(path.join(dir, '.build-commit'), 'not-a-commit')
    expect(() => resolveBuildCommit(dir, true)).toThrow('Missing Studio commit')
  })
})
