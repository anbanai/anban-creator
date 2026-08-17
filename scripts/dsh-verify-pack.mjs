import { readFile, readdir } from 'node:fs/promises'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { parsePackResult } from '../plugins/dsh/scripts/package-integrity.mjs'

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
