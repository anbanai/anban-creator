import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import {
  runBoundedCommand,
  terminateWindowsTree,
} from './dsh-process-supervisor.mjs'

function processExists(pid) {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    if (error?.code === 'ESRCH') return false
    throw error
  }
}

async function waitForFile(path) {
  const deadline = Date.now() + 2_000
  while (Date.now() < deadline) {
    try {
      return Number.parseInt(await readFile(path, 'utf8'), 10)
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error
      await new Promise((resolveWait) => setTimeout(resolveWait, 20))
    }
  }
  throw new Error(`timed out waiting for ${path}`)
}

async function waitForProcessesGone(pids) {
  const deadline = Date.now() + 3_000
  while (Date.now() < deadline) {
    if (pids.every((pid) => !processExists(pid))) return
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  assert.fail(`process tree still alive: ${pids.filter(processExists).join(', ')}`)
}

test(
  'timeout terminates a resistant parent and descendant before rejecting',
  { skip: process.platform === 'win32' },
  async () => {
    const root = await mkdtemp(join(tmpdir(), 'dsh-supervisor-test-'))
    const parentPidPath = join(root, 'parent.pid')
    const childPidPath = join(root, 'child.pid')
    const fixturePath = join(root, 'resistant-tree.mjs')
    await writeFile(
      fixturePath,
      `import { spawn } from 'node:child_process'\n` +
        `import { writeFileSync } from 'node:fs'\n` +
        `writeFileSync(${JSON.stringify(parentPidPath)}, String(process.pid))\n` +
        `process.on('SIGTERM', () => {})\n` +
        `const child = spawn(process.execPath, ['-e', ${JSON.stringify("process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)")}], { stdio: 'ignore' })\n` +
        `writeFileSync(${JSON.stringify(childPidPath)}, String(child.pid))\n` +
        `setInterval(() => {}, 1000)\n`,
    )

    try {
      await assert.rejects(
        runBoundedCommand(process.execPath, [fixturePath], {
          label: 'resistant tree',
          timeoutMs: 150,
          terminationGraceMs: 50,
          closeWatchdogMs: 2_000,
        }),
        /resistant tree timed out after 150ms/,
      )
      const pids = [
        await waitForFile(parentPidPath),
        await waitForFile(childPidPath),
      ]
      await waitForProcessesGone(pids)
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  },
)

test(
  'timeout kills a resistant descendant after its parent exits on TERM',
  { skip: process.platform === 'win32' },
  async () => {
    const root = await mkdtemp(join(tmpdir(), 'dsh-supervisor-orphan-test-'))
    const childPidPath = join(root, 'child.pid')
    const fixturePath = join(root, 'cooperative-parent.mjs')
    let childPid
    await writeFile(
      fixturePath,
      `import { spawn } from 'node:child_process'\n` +
        `import { writeFileSync } from 'node:fs'\n` +
        `const child = spawn(process.execPath, ['-e', ${JSON.stringify("process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)")}], { stdio: 'ignore' })\n` +
        `writeFileSync(${JSON.stringify(childPidPath)}, String(child.pid))\n` +
        `setInterval(() => {}, 1000)\n`,
    )

    try {
      await assert.rejects(
        runBoundedCommand(process.execPath, [fixturePath], {
          label: 'orphan-resistant tree',
          timeoutMs: 150,
          terminationGraceMs: 50,
          closeWatchdogMs: 2_000,
        }),
        /orphan-resistant tree timed out after 150ms/,
      )
      childPid = await waitForFile(childPidPath)
      await waitForProcessesGone([childPid])
    } finally {
      if (childPid !== undefined && processExists(childPid)) {
        process.kill(childPid, 'SIGKILL')
      }
      await rm(root, { force: true, recursive: true })
    }
  },
)

test('Windows termination awaits taskkill tree operations', async () => {
  const invocations = []
  const spawnProcess = (command, args, options) => {
    invocations.push({ args, command, options })
    const child = new EventEmitter()
    queueMicrotask(() => child.emit('close', 0, null))
    return child
  }

  await terminateWindowsTree(42, false, { spawnProcess })
  await terminateWindowsTree(42, true, { spawnProcess })

  assert.deepEqual(
    invocations.map(({ args, command }) => ({ args, command })),
    [
      { command: 'taskkill', args: ['/PID', '42', '/T'] },
      { command: 'taskkill', args: ['/PID', '42', '/T', '/F'] },
    ],
  )
  assert.ok(
    invocations.every(
      ({ options }) =>
        options.shell === false &&
        options.stdio === 'ignore' &&
        options.windowsHide === true,
    ),
  )
})
