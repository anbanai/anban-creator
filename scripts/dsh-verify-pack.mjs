import { readFile, readdir } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

function parsePackResult(source) {
  let parsed
  try {
    parsed = JSON.parse(source)
  } catch {
    throw new Error('pnpm pack metadata is not valid JSON')
  }
  if (
    !Array.isArray(parsed) ||
    parsed.length !== 1 ||
    parsed[0] === null ||
    typeof parsed[0] !== 'object' ||
    typeof parsed[0].name !== 'string' ||
    typeof parsed[0].version !== 'string' ||
    typeof parsed[0].filename !== 'string' ||
    !Array.isArray(parsed[0].files) ||
    !parsed[0].files.every(
      (file) =>
        file !== null &&
        typeof file === 'object' &&
        typeof file.path === 'string',
    )
  ) {
    throw new Error('pnpm pack metadata does not contain one complete result')
  }
  return parsed[0]
}

export async function verifyPackResult(metadataPath, expectedVersion) {
  const absoluteMetadataPath = resolve(metadataPath)
  const packDirectory = dirname(absoluteMetadataPath)
  const result = parsePackResult(await readFile(absoluteMetadataPath, 'utf8'))
  const tarballs = (await readdir(packDirectory)).filter((name) =>
    name.endsWith('.tgz'),
  )

  if (
    result.name !== '@anban/dsh-plugin' ||
    result.version !== expectedVersion ||
    tarballs.length !== 1 ||
    tarballs[0] !== basename(result.filename)
  ) {
    throw new Error('Desktop acceptance artifact is not the exact pack result')
  }

  return resolve(packDirectory, tarballs[0])
}

const invokedPath = process.argv[1] === undefined ? '' : resolve(process.argv[1])
if (invokedPath === fileURLToPath(import.meta.url)) {
  const [, , metadataPath, expectedVersion] = process.argv
  if (metadataPath === undefined || expectedVersion === undefined) {
    throw new Error('usage: dsh-verify-pack.mjs <pack.json> <version>')
  }
  await verifyPackResult(metadataPath, expectedVersion)
}
