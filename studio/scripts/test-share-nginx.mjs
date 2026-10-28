import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

test('Nginx protects share routes after path normalization and SPA fallback', async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'anban-share-nginx-'))
  const name = `anban-share-nginx-${process.pid}`
  const docker = (...args) => execFileSync('docker', args, { encoding: 'utf8', timeout: 30_000 })
  try {
    await writeFile(join(directory, 'upstream.conf'), 'server { listen 8081; access_log off; location / { default_type application/json; return 200 \'{"ok":true}\'; } }\n')
    docker('run', '-d', '--rm', '--pull=never', '--name', name, '-p', '127.0.0.1::80',
      '-e', 'BACKEND_SCHEME=http', '-e', 'BACKEND_HOST=127.0.0.1', '-e', 'BACKEND_PORT=8081', '-e', 'NGINX_ENVSUBST_FILTER=BACKEND_',
      '-v', `${fileURLToPath(new URL('../default.conf.template', import.meta.url))}:/etc/nginx/templates/default.conf.template:ro`,
      '-v', `${join(directory, 'upstream.conf')}:/etc/nginx/conf.d/upstream.conf:ro`, 'nginx:1.24-alpine')
    const address = docker('port', name, '80/tcp').trim()
    const base = `http://${address}`
    let ready = false
    for (let attempt = 0; attempt < 40; attempt++) {
      try { ready = (await fetch(`${base}/index.html`)).ok; if (ready) break } catch { /* Nginx is starting. */ }
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
    assert.ok(ready, 'Nginx should start with the actual Studio template')
    docker('exec', name, 'nginx', '-t')

    const paths = [
      '/share/private-share-canonical',
      '/ShArE/private-share-mixed-case',
      '/%73hare/private-share-encoded-character',
      '/share%2Fprivate-share-encoded-slash',
      '/%73hare%2fprivate-share-encoded-both',
      '/api/v1/public/shares/private-share-api-canonical',
      '/api/v1/public/ShArEs/private-share-api-mixed-case',
      '/api/v1/public/%73hares/private-share-api-encoded-character',
      '/api/v1/public/shares%2fprivate-share-api-encoded-slash',
      '/api/v1/public%2f%73hares%2Fprivate-share-api-encoded-both',
    ]
    for (const path of paths) {
      await t.test(path, async () => {
        const response = await fetch(`${base}${path}`)
        assert.equal(response.status, 200)
        if (path.startsWith('/api/')) assert.deepEqual(await response.json(), { ok: true })
        else {
          assert.match(await response.text(), /<html/)
          assert.equal(response.headers.get('cache-control'), 'no-store, must-revalidate')
          assert.equal(response.headers.get('x-robots-tag'), 'noindex, nofollow')
          assert.equal(response.headers.get('referrer-policy'), 'no-referrer')
        }
      })
    }
    const ordinary = await fetch(`${base}/plugins?ordinary-log-control=1`)
    assert.equal(ordinary.status, 200)
    assert.equal(ordinary.headers.get('x-robots-tag'), null)
    await ordinary.text()
    const logs = docker('logs', name)
    assert.match(logs, /GET \/plugins\?ordinary-log-control=1 HTTP\/1.1/, 'ordinary requests must remain logged')
    assert.doesNotMatch(logs, /private-share-/, 'normalized share paths must never expose tokens in access logs')
  } finally {
    try { docker('rm', '-f', name) } finally { await rm(directory, { recursive: true, force: true }) }
  }
})
