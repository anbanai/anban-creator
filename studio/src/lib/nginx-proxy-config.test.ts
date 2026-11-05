import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Studio static hosting contract', () => {
  it('pins the Studio Service to the active blue/green release', () => {
    const deployment = readFileSync('Deployment.yaml', 'utf8')
    const service = deployment.match(/kind: Service[\s\S]*?\n---/)?.[0]

    expect(service).toContain('selector:')
    expect(service).toContain('app: ${micro_service_name}')
    expect(service).toContain('version: ${version_switch}')
  })

  it('revalidates HTML while keeping hashed assets immutable', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const htmlLocation = template.match(/location = \/index\.html \{([\s\S]*?)\n    \}/)?.[1]
    const assetLocation = template.match(/location \/assets\/ \{([\s\S]*?)\n    \}/)?.[1]

    expect(htmlLocation).toContain('add_header Cache-Control "no-store, must-revalidate" always;')
    expect(assetLocation).toContain('add_header Cache-Control "public, immutable";')
  })
})
