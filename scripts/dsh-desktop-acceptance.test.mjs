import assert from 'node:assert/strict'
import { createServer } from 'node:net'
import test from 'node:test'

import { fetchWithDeadline } from './dsh-desktop-acceptance.mjs'

test('Desktop loopback probes abort a server that accepts without responding', async () => {
  const sockets = new Set()
  const server = createServer((socket) => {
    sockets.add(socket)
    socket.once('close', () => sockets.delete(socket))
  })
  await new Promise((resolveListen, rejectListen) => {
    server.once('error', rejectListen)
    server.listen(0, '127.0.0.1', resolveListen)
  })
  const address = server.address()
  assert.ok(typeof address === 'object' && address !== null)

  const started = Date.now()
  try {
    await assert.rejects(
      fetchWithDeadline(
        `http://127.0.0.1:${String(address.port)}/stalled`,
        Date.now() + 100,
      ),
      (error) => error?.name === 'TimeoutError' || error?.name === 'AbortError',
    )
    assert.ok(Date.now() - started < 1_000, 'stalled probe exceeded its deadline')
  } finally {
    for (const socket of sockets) socket.destroy()
    await new Promise((resolveClose, rejectClose) =>
      server.close((error) =>
        error === undefined ? resolveClose() : rejectClose(error),
      ),
    )
  }
})
