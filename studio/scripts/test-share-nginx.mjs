import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

test('Nginx protects share routes after path normalization and SPA fallback', async (t) => {
  const name = `anban-share-nginx-${process.pid}`
  const docker = (...args) => execFileSync('docker', args, { encoding: 'utf8', timeout: 30_000 })
  try {
    docker('run', '-d', '--rm', '--pull=never', '--name', name, '-p', '127.0.0.1::80',
      '-v', `${fileURLToPath(new URL('../default.conf.template', import.meta.url))}:/etc/nginx/conf.d/default.conf:ro`, 'nginx:1.24-alpine')
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
        if (path.startsWith('/api/')) assert.equal(response.status, 404)
        else {
          assert.equal(response.status, 200)
          assert.match(await response.text(), /<html/)
          assert.equal(response.headers.get('cache-control'), 'no-store, must-revalidate')
          assert.equal(response.headers.get('x-robots-tag'), 'noindex, nofollow')
          assert.equal(response.headers.get('referrer-policy'), 'no-referrer')
        }
      })
    }
    for (const path of ['/api', '/api/v1/tasks', '/mcp', '/mcp/', '/ws', '/ws/events', '/archestra', '/archestra/']) {
      assert.equal((await fetch(`${base}${path}`)).status, 404, `${path} must not be served by Studio`)
    }
    const ordinary = await fetch(`${base}/plugins?ordinary-log-control=1`)
    assert.equal(ordinary.status, 200)
    assert.equal(ordinary.headers.get('x-robots-tag'), null)
    await ordinary.text()
    const logs = docker('logs', name)
    assert.match(logs, /GET \/plugins\?ordinary-log-control=1 HTTP\/1.1/, 'ordinary requests must remain logged')
    assert.doesNotMatch(logs, /private-share-/, 'normalized share paths must never expose tokens in access logs')
  } finally {
    docker('rm', '-f', name)
  }
})
