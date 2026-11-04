import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Studio nginx backend proxy contract', () => {
  it('uses HTTP port 8080 for local and ACK backend services', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const dockerfile = readFileSync('../deploy/docker/Dockerfile.studio', 'utf8')
    const deployment = readFileSync('Deployment.yaml', 'utf8').replace(/\r\n/g, '\n')

    expect(template).toContain('proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};')
    expect(template).not.toContain('proxy_pass http://${BACKEND_HOST}:8080;')

    expect(dockerfile).toContain('ENV BACKEND_SCHEME=http')
    expect(dockerfile).toContain('ENV BACKEND_PORT=8080')

    expect(deployment).toContain('- name: BACKEND_SCHEME\n              value: http')
    expect(deployment).toContain('- name: BACKEND_PORT\n              value: "8080"')
  })

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

  it('serves the Archestra web app on its own host instead of a sub-path', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const dockerfile = readFileSync('../deploy/docker/Dockerfile.studio', 'utf8')
    const deployment = readFileSync('Deployment.yaml', 'utf8').replace(/\r\n/g, '\n')
    const archestraServer = template.match(
      /server_name \$\{ARCHESTRA_PUBLIC_HOST\};([\s\S]*?)\n\}/,
    )?.[1]

    // Next.js emits root-absolute URLs, so the proxy must own a hostname.
    expect(archestraServer).toBeDefined()
    expect(archestraServer).toContain(
      'proxy_pass http://${ARCHESTRA_HOST}:${ARCHESTRA_PORT};',
    )
    expect(archestraServer).toContain('client_max_body_size 200m;')
    expect(archestraServer).toContain('proxy_set_header Upgrade $http_upgrade;')
    expect(archestraServer).toContain(
      'proxy_set_header Connection $archestra_connection_upgrade;',
    )
    expect(archestraServer).toContain(
      'proxy_set_header X-Forwarded-Proto ${ARCHESTRA_PUBLIC_SCHEME};',
    )
    expect(archestraServer).toContain('proxy_buffering off;')
    expect(archestraServer).toContain('proxy_cache off;')
    expect(archestraServer).toContain('proxy_read_timeout 1h;')
    expect(archestraServer).toContain('proxy_send_timeout 1h;')

    // /archestra remains an entry URL, but only as a redirect to that host.
    expect(template).toContain('location ~ ^/archestra/?$ {')
    expect(template).toContain(
      'return 302 ${ARCHESTRA_PUBLIC_SCHEME}://${ARCHESTRA_PUBLIC_HOST}/;',
    )

    expect(dockerfile).toContain('ENV ARCHESTRA_HOST=127.0.0.1')
    expect(dockerfile).toContain('ENV ARCHESTRA_PORT=3000')
    expect(dockerfile).toContain('ENV ARCHESTRA_PUBLIC_SCHEME=http')
    expect(dockerfile).toContain('ENV ARCHESTRA_PUBLIC_HOST=archestra.localhost')

    expect(deployment).toContain(
      '- name: ARCHESTRA_HOST\n              value: archestra-platform.anbanai-prod.svc.cluster.local',
    )
    expect(deployment).toContain('- name: ARCHESTRA_PORT\n              value: "3000"')
    expect(deployment).toContain(
      '- name: ARCHESTRA_PUBLIC_SCHEME\n              value: https',
    )
    expect(deployment).toContain(
      '- name: ARCHESTRA_PUBLIC_HOST\n              value: archestra.anbanai.com',
    )

    // The Studio release owns the ALB host rule; no console edit is needed.
    const ingress = deployment.match(/kind: Ingress([\s\S]*)$/)?.[0]
    expect(ingress).toBeDefined()
    expect(ingress).toContain('name: ${micro_service_name}-archestra')
    expect(ingress).toContain('ingressClassName: alb')
    expect(ingress).toContain(
      `alb.ingress.kubernetes.io/listen-ports: '[{"HTTPS": 443}]'`,
    )
    expect(ingress).toContain('host: archestra.anbanai.com')
    expect(ingress).toContain('name: ${micro_service_name}-svc')
    expect(ingress).toContain('number: 8080')
  })
})
