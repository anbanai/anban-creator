import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import {
  runBoundedCommand,
  spawnProcessTree,
  terminateProcessTree,
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

test(
  'output overflow removes the real root and descendant process identities',
  async () => {
    const root = await mkdtemp(join(tmpdir(), 'dsh-supervisor-output-test-'))
    const parentPidPath = join(root, 'parent.pid')
    const childPidPath = join(root, 'child.pid')
    const fixturePath = join(root, 'output-tree.mjs')
    let pids = []
    await writeFile(
      fixturePath,
      `import { spawn } from 'node:child_process'\n` +
        `import { writeFileSync } from 'node:fs'\n` +
        `writeFileSync(${JSON.stringify(parentPidPath)}, String(process.pid))\n` +
        `process.on('SIGTERM', () => {})\n` +
        `const child = spawn(process.execPath, ['-e', ${JSON.stringify("process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)")}], { stdio: 'ignore' })\n` +
        `writeFileSync(${JSON.stringify(childPidPath)}, String(child.pid))\n` +
        `process.stdout.write('x'.repeat(4096))\n` +
        `setInterval(() => {}, 1000)\n`,
    )

    try {
      await assert.rejects(
        runBoundedCommand(process.execPath, [fixturePath], {
          closeWatchdogMs: 3_000,
          label: 'overflow tree',
          maxBuffer: 64,
          terminationGraceMs: 100,
          timeoutMs: 10_000,
        }),
        /overflow tree exceeded output limit/,
      )
      pids = [await waitForFile(parentPidPath), await waitForFile(childPidPath)]
      await waitForProcessesGone(pids)
    } finally {
      for (const pid of pids.filter(processExists)) {
        try {
          process.kill(pid, 'SIGKILL')
        } catch {
          // The assertion above reports the primary cleanup failure.
        }
      }
      await rm(root, { force: true, recursive: true })
    }
  },
)

test(
  'Windows timeout removes the real root and descendant process identities',
  { skip: process.platform !== 'win32' },
  async () => {
    const root = await mkdtemp(join(tmpdir(), 'dsh-supervisor-windows-test-'))
    const parentPidPath = join(root, 'parent.pid')
    const childPidPath = join(root, 'child.pid')
    const fixturePath = join(root, 'windows-tree.mjs')
    let pids = []
    await writeFile(
      fixturePath,
      `import { spawn } from 'node:child_process'\n` +
        `import { writeFileSync } from 'node:fs'\n` +
        `writeFileSync(${JSON.stringify(parentPidPath)}, String(process.pid))\n` +
        `const child = spawn(process.execPath, ['-e', ${JSON.stringify('setInterval(() => {}, 1000)')}], { stdio: 'ignore' })\n` +
        `writeFileSync(${JSON.stringify(childPidPath)}, String(child.pid))\n` +
        `setInterval(() => {}, 1000)\n`,
    )

    try {
      await assert.rejects(
        runBoundedCommand(process.execPath, [fixturePath], {
          label: 'real Windows tree',
          timeoutMs: 1_000,
          terminationGraceMs: 100,
          closeWatchdogMs: 3_000,
        }),
        /real Windows tree timed out after 1000ms/,
      )
      pids = [await waitForFile(parentPidPath), await waitForFile(childPidPath)]
      await waitForProcessesGone(pids)
    } finally {
      for (const pid of pids.filter(processExists)) {
        try {
          process.kill(pid, 'SIGKILL')
        } catch {
          // The assertion above reports the primary cleanup failure.
        }
      }
      await rm(root, { force: true, recursive: true })
    }
  },
)

test(
  'Windows Job Object preserves argv, cwd, environment, and stdio',
  { skip: process.platform !== 'win32' },
  async () => {
    const root = await mkdtemp(join(tmpdir(), 'dsh-supervisor-windows-io-test-'))
    try {
      const expectedArgs = ['two words', 'quote"value', 'trailing\\', '']
      const result = await runBoundedCommand(
        process.execPath,
        [
          '-e',
          "process.stdout.write(JSON.stringify({ args: process.argv.slice(1), cwd: process.cwd(), marker: process.env.DSH_JOB_MARKER })); process.stderr.write('stderr-ok')",
          ...expectedArgs,
        ],
        {
          cwd: root,
          env: { ...process.env, DSH_JOB_MARKER: 'environment-ok' },
          label: 'Windows Job Object stdio fixture',
          timeoutMs: 10_000,
        },
      )

      assert.equal(result.status, 0)
      assert.equal(result.stderr, 'stderr-ok')
      assert.deepEqual(JSON.parse(result.stdout), {
        args: expectedArgs,
        cwd: root,
        marker: 'environment-ok',
      })
    } finally {
      await rm(root, { force: true, recursive: true })
    }
  },
)

test('Windows spawn and cleanup stay bound to a kill-on-close Job Object', async () => {
  const invocations = []
  const signals = []
  const spawnProcess = (command, args, options) => {
    invocations.push({ args, command, options })
    const child = new EventEmitter()
    child.exitCode = null
    child.pid = 42
    child.signalCode = null
    child.kill = (signal) => {
      signals.push(signal)
      child.signalCode = signal
      queueMicrotask(() => child.emit('close', null, signal))
      return true
    }
    return child
  }
  const child = spawnProcessTree('C:\\Program Files\\fixture.exe', ['--label', 'two words'], {
    platform: 'win32',
    spawnProcess,
  })

  assert.equal(invocations.length, 1)
  assert.equal(invocations[0].command, 'powershell.exe')
  const payloadIndex = invocations[0].args.indexOf('-Payload') + 1
  assert.ok(payloadIndex > 0, 'Job Object wrapper is missing its payload')
  assert.deepEqual(
    JSON.parse(Buffer.from(invocations[0].args[payloadIndex], 'base64url')),
    {
      args: ['--label', 'two words'],
      command: 'C:\\Program Files\\fixture.exe',
    },
  )

  await terminateProcessTree(child, {
    closeWatchdogMs: 100,
    platform: 'win32',
    terminationGraceMs: 1,
  })
  assert.deepEqual(signals, ['SIGKILL'])
  assert.equal(invocations.length, 1, 'cleanup launched a PID-based helper')
})

test('Windows Job Object cleanup is bounded when the wrapper never closes', async () => {
  const child = new EventEmitter()
  child.exitCode = null
  child.pid = 42
  child.signalCode = null
  child.kill = (signal) => signal === 'SIGKILL'
  const started = Date.now()
  await assert.rejects(
    Promise.race([
      terminateProcessTree(child, {
        closeWatchdogMs: 30,
        label: 'stuck wrapper',
        platform: 'win32',
        terminationGraceMs: 10,
      }),
      new Promise((_, rejectWait) =>
        setTimeout(() => rejectWait(new Error('external test watchdog expired')), 150),
      ),
    ]),
    /stuck wrapper Windows Job Object did not close/,
  )
  assert.ok(
    Date.now() - started < 100,
    'Job Object wrapper exceeded its total cleanup deadline',
  )
})

test('Windows Job Object cleanup accepts a close racing a failed kill', async () => {
  const child = new EventEmitter()
  child.exitCode = null
  child.pid = 42
  child.signalCode = null
  child.kill = () => {
    queueMicrotask(() => {
      child.exitCode = 0
      child.emit('close', 0, null)
    })
    return false
  }

  await terminateProcessTree(child, {
    closeWatchdogMs: 100,
    platform: 'win32',
    terminationGraceMs: 1,
  })
})
