import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import {
  runBoundedCommand,
  terminateProcessTree,
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
        if (process.platform === 'win32') {
          try {
            await terminateWindowsTree(pid, true)
          } catch {
            // The assertion above reports the primary cleanup failure.
          }
        } else {
          process.kill(pid, 'SIGKILL')
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
          await terminateWindowsTree(pid, true)
        } catch {
          // The assertion above reports the primary cleanup failure.
        }
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

test('Windows cleanup retains and force-kills descendants after the root exits', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const descendant = { creationDate: 'child-created', parentPid: 42, pid: 84 }
  let rows = [root, descendant]
  const child = {
    exitCode: null,
    kill() {
      assert.fail('Windows cleanup must not use direct child.kill')
    },
    pid: 42,
    signalCode: null,
  }
  const invocations = []
  const spawnProcess = (command, args, options) => {
    invocations.push({ args, command, options })
    const killer = new EventEmitter()
    queueMicrotask(() => {
      const pid = Number.parseInt(args[1], 10)
      if (args.includes('/F')) {
        rows = rows.filter((row) => row.pid !== pid)
      } else {
        rows = rows.filter((row) => row.pid !== 42)
        child.exitCode = 0
      }
      killer.emit('close', 0, null)
    })
    return killer
  }

  await terminateProcessTree(child, {
    closeWatchdogMs: 100,
    platform: 'win32',
    queryWindowsProcesses: async () => rows,
    snapshotWindowsTree: async () => [root, descendant],
    spawnProcess,
    terminationGraceMs: 1,
  })

  assert.deepEqual(rows, [])
  assert.ok(
    invocations.some(
      ({ args }) => args[1] === '84' && args.includes('/T') && args.includes('/F'),
    ),
    'forced cleanup must target the retained descendant PID',
  )
})

test('Windows cleanup reports failed taskkill status for surviving identities', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const child = {
    exitCode: null,
    pid: 42,
    signalCode: null,
  }
  const spawnProcess = () => {
    const killer = new EventEmitter()
    queueMicrotask(() => killer.emit('close', 5, null))
    return killer
  }

  await assert.rejects(
    terminateProcessTree(child, {
      closeWatchdogMs: 1,
      label: 'failed Windows tree',
      platform: 'win32',
      queryWindowsProcesses: async () => [root],
      snapshotWindowsTree: async () => [root],
      spawnProcess,
      terminationGraceMs: 1,
    }),
    /taskkill failed with status 5.*PIDs: 42/,
  )
})

test('Windows cleanup does not kill a reused PID with a different creation identity', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const descendant = { creationDate: 'child-created', parentPid: 42, pid: 84 }
  let rows = [root, descendant]
  const invocations = []
  const spawnProcess = (command, args) => {
    invocations.push({ args, command })
    const killer = new EventEmitter()
    queueMicrotask(() => {
      if (!args.includes('/F')) {
        rows = [{ creationDate: 'replacement-created', parentPid: 1, pid: 84 }]
      }
      killer.emit('close', 0, null)
    })
    return killer
  }

  await terminateProcessTree({ exitCode: null, pid: 42, signalCode: null }, {
    closeWatchdogMs: 100,
    platform: 'win32',
    queryWindowsProcesses: async () => rows,
    snapshotWindowsTree: async () => [root, descendant],
    spawnProcess,
    terminationGraceMs: 1,
  })

  assert.ok(
    !invocations.some(({ args }) => args[1] === '84' && args.includes('/F')),
    'cleanup force-killed an unrelated process that reused the descendant PID',
  )
})

test('Windows cleanup repeatedly captures a late descendant after its parent exits', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const descendant = { creationDate: 'child-created', parentPid: 42, pid: 84 }
  const late = { creationDate: 'late-created', parentPid: 84, pid: 126 }
  let rows = [root, descendant]
  const invocations = []
  const spawnProcess = (command, args) => {
    invocations.push({ args, command })
    const killer = new EventEmitter()
    queueMicrotask(() => {
      if (!args.includes('/F')) rows = [late]
      if (args.includes('/F') && args[1] === '126') rows = []
      killer.emit('close', 0, null)
    })
    return killer
  }

  await terminateProcessTree({ exitCode: null, pid: 42, signalCode: null }, {
    closeWatchdogMs: 100,
    platform: 'win32',
    queryWindowsProcesses: async () => rows,
    snapshotWindowsTree: async () => [root, descendant],
    spawnProcess,
    terminationGraceMs: 1,
  })

  assert.ok(
    invocations.some(
      ({ args }) => args[1] === '126' && args.includes('/T') && args.includes('/F'),
    ),
    'cleanup did not retain and force the late descendant identity',
  )
})

test('Windows cleanup requires a stable empty tree before returning', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const late = { creationDate: 'late-created', parentPid: 42, pid: 126 }
  let queryCount = 0
  let lateAlive = true
  const invocations = []
  const spawnProcess = (command, args) => {
    invocations.push({ args, command })
    const killer = new EventEmitter()
    queueMicrotask(() => {
      if (args.includes('/F') && args[1] === '126') lateAlive = false
      killer.emit('close', 0, null)
    })
    return killer
  }

  await terminateProcessTree({ exitCode: null, pid: 42, signalCode: null }, {
    closeWatchdogMs: 200,
    platform: 'win32',
    queryWindowsProcesses: async () => {
      queryCount += 1
      if (queryCount === 1) return []
      return lateAlive ? [late] : []
    },
    snapshotWindowsTree: async () => [root],
    spawnProcess,
    terminationGraceMs: 1,
  })

  assert.ok(queryCount >= 2, 'cleanup accepted a single empty process snapshot')
  assert.ok(
    invocations.some(
      ({ args }) => args[1] === '126' && args.includes('/T') && args.includes('/F'),
    ),
    'cleanup missed the descendant that appeared after one empty snapshot',
  )
})

test('Windows taskkill helper is bounded when the subprocess never closes', async () => {
  let killed = false
  const spawnProcess = () => {
    const killer = new EventEmitter()
    killer.kill = () => {
      killed = true
    }
    return killer
  }

  await assert.rejects(
    Promise.race([
      terminateWindowsTree(42, true, { spawnProcess, timeoutMs: 10 }),
      new Promise((_, rejectWait) =>
        setTimeout(() => rejectWait(new Error('external test watchdog expired')), 100),
      ),
    ]),
    /taskkill.*timed out/,
  )
  assert.equal(killed, true)
})

test('Windows process queries use the cleanup watchdog rather than the grace delay', async () => {
  const root = { creationDate: 'root-created', parentPid: 1, pid: 42 }
  const observedTimeouts = []
  const spawnProcess = () => {
    const killer = new EventEmitter()
    queueMicrotask(() => killer.emit('close', 0, null))
    return killer
  }

  await terminateProcessTree({ exitCode: null, pid: 42, signalCode: null }, {
    closeWatchdogMs: 100,
    platform: 'win32',
    queryWindowsProcesses: async ({ timeoutMs }) => {
      observedTimeouts.push(timeoutMs)
      return []
    },
    snapshotWindowsTree: async () => [root],
    spawnProcess,
    terminationGraceMs: 1,
  })

  assert.ok(
    observedTimeouts.every((timeoutMs) => timeoutMs > 1),
    `query timeouts were constrained by grace: ${observedTimeouts.join(', ')}`,
  )
})
