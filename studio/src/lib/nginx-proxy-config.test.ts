import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Studio nginx backend proxy contract', () => {
  it('keeps local HTTP defaults while ACK selects the TLS service', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const dockerfile = readFileSync('Dockerfile', 'utf8')
    const deployment = readFileSync('Deployment.yaml', 'utf8')

    expect(template).toContain('proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};')
    expect(template).not.toContain('proxy_pass http://${BACKEND_HOST}:8080;')

    expect(dockerfile).toContain('ENV BACKEND_SCHEME=http')
    expect(dockerfile).toContain('ENV BACKEND_PORT=8080')

    expect(deployment).toContain('- name: BACKEND_SCHEME\n              value: https')
    expect(deployment).toContain('- name: BACKEND_PORT\n              value: "8443"')
  })
})
