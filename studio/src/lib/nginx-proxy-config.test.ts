import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Studio nginx backend proxy contract', () => {
  it('keeps local HTTP defaults while ACK selects the TLS service', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const dockerfile = readFileSync('../deploy/docker/Dockerfile.studio', 'utf8')
    const deployment = readFileSync('Deployment.yaml', 'utf8')

    expect(template).toContain('proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};')
    expect(template).not.toContain('proxy_pass http://${BACKEND_HOST}:8080;')

    expect(dockerfile).toContain('ENV BACKEND_SCHEME=http')
    expect(dockerfile).toContain('ENV BACKEND_PORT=8080')

    expect(deployment).toContain('- name: BACKEND_SCHEME\n              value: https')
    expect(deployment).toContain('- name: BACKEND_PORT\n              value: "8443"')
  })

  it('proxies the exact MCP endpoint to the backend as an unbuffered HTTP stream', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const mcpLocation = template.match(/location = \/mcp \{([\s\S]*?)\n    \}/)?.[1]

    expect(mcpLocation).toBeDefined()
    expect(mcpLocation).toContain(
      'proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};',
    )
    expect(mcpLocation).toContain('proxy_http_version 1.1;')
    expect(mcpLocation).toContain('proxy_set_header Host $host;')
    expect(mcpLocation).toContain('proxy_set_header X-Forwarded-Proto $scheme;')
    expect(mcpLocation).toContain('proxy_buffering off;')
    expect(mcpLocation).toContain('proxy_cache off;')
    expect(mcpLocation).toContain('proxy_read_timeout 1h;')
    expect(mcpLocation).toContain('proxy_send_timeout 1h;')
    expect(mcpLocation).not.toContain('proxy_set_header Authorization')
  })
})
