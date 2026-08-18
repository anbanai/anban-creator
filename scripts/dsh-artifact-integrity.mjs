import { createHash } from 'node:crypto'
import { createReadStream } from 'node:fs'
import { lstat, readFile, writeFile } from 'node:fs/promises'
import { basename, dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { verifyPackResult } from './dsh-verify-pack.mjs'

const DIGEST_FILENAME = 'artifact.sha256.json'

async function sha256(path) {
  const hash = createHash('sha256')
  for await (const chunk of createReadStream(path)) hash.update(chunk)
  return hash.digest('hex')
}

async function requireRegularFile(path, label) {
  const status = await lstat(path)
  if (!status.isFile() || status.isSymbolicLink()) {
    throw new Error(`${label} must be a regular file`)
  }
}

function digestPathFor(metadataPath, digestPath) {
  const expected = join(dirname(resolve(metadataPath)), DIGEST_FILENAME)
  if (resolve(digestPath) !== expected) {
    throw new Error(`artifact digest must be ${DIGEST_FILENAME} beside pack.json`)
  }
  return expected
}

function parseDigestManifest(source) {
  let manifest
  try {
    manifest = JSON.parse(source)
  } catch {
    throw new Error('artifact digest is not valid JSON')
  }
  if (
    manifest === null ||
    typeof manifest !== 'object' ||
    Array.isArray(manifest) ||
    JSON.stringify(Object.keys(manifest).sort()) !==
      JSON.stringify([
        'algorithm',
        'metadata',
        'metadataSha256',
        'tarball',
        'tarballSha256',
      ]) ||
    manifest.algorithm !== 'sha256' ||
    manifest.metadata !== 'pack.json' ||
    typeof manifest.metadataSha256 !== 'string' ||
    !/^[0-9a-f]{64}$/.test(manifest.metadataSha256) ||
    typeof manifest.tarball !== 'string' ||
    basename(manifest.tarball) !== manifest.tarball ||
    typeof manifest.tarballSha256 !== 'string' ||
    !/^[0-9a-f]{64}$/.test(manifest.tarballSha256)
  ) {
    throw new Error('artifact digest does not match the strict schema')
  }
  return manifest
}

export async function createArtifactDigest(
  metadataPath,
  expectedVersion,
  digestPath,
) {
  const tarball = await verifyPackResult(metadataPath, expectedVersion)
  await requireRegularFile(tarball, 'DSH release tarball')
  const destination = digestPathFor(metadataPath, digestPath)
  await writeFile(
    destination,
    `${JSON.stringify({
      algorithm: 'sha256',
      metadata: basename(resolve(metadataPath)),
      metadataSha256: await sha256(resolve(metadataPath)),
      tarball: basename(tarball),
      tarballSha256: await sha256(tarball),
    })}\n`,
    { encoding: 'utf8', flag: 'wx', mode: 0o600 },
  )
}

export async function verifyArtifactDigest(
  metadataPath,
  expectedVersion,
  digestPath,
) {
  const tarball = await verifyPackResult(metadataPath, expectedVersion)
  await requireRegularFile(tarball, 'DSH release tarball')
  const source = digestPathFor(metadataPath, digestPath)
  await requireRegularFile(source, 'DSH release artifact digest')
  const manifest = parseDigestManifest(await readFile(source, 'utf8'))
  if (
    manifest.metadataSha256 !== (await sha256(resolve(metadataPath))) ||
    manifest.tarball !== basename(tarball) ||
    manifest.tarballSha256 !== (await sha256(tarball))
  ) {
    throw new Error('DSH release artifact digest mismatch')
  }
  return tarball
}

const invokedPath = process.argv[1] === undefined ? '' : resolve(process.argv[1])
if (invokedPath === fileURLToPath(import.meta.url)) {
  const [, , action, metadataPath, expectedVersion, digestPath] = process.argv
  if (
    !['create', 'verify'].includes(action) ||
    metadataPath === undefined ||
    expectedVersion === undefined ||
    digestPath === undefined
  ) {
    throw new Error(
      'usage: dsh-artifact-integrity.mjs <create|verify> <pack.json> <version> <artifact.sha256.json>',
    )
  }
  if (action === 'create') {
    await createArtifactDigest(metadataPath, expectedVersion, digestPath)
  } else {
    await verifyArtifactDigest(metadataPath, expectedVersion, digestPath)
  }
}
