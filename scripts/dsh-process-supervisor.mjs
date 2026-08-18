import { spawn } from 'node:child_process'

const DEFAULT_TERMINATION_GRACE_MS = 1_000
const DEFAULT_CLOSE_WATCHDOG_MS = 10_000
const DEFAULT_TASKKILL_TIMEOUT_MS = 5_000
const WINDOWS_SNAPSHOT_TIMEOUT_MS = 5_000

function hasExited(child) {
  return child.exitCode !== null || child.signalCode !== null
}

function posixProcessGroupExists(pid, killProcess) {
  try {
    killProcess(-pid, 0)
    return true
  } catch (error) {
    if (error?.code === 'ESRCH') return false
    if (error?.code === 'EPERM') return true
    throw error
  }
}

async function treeExitsWithin(child, timeoutMs, platform, killProcess) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const gone =
      platform === 'win32'
        ? hasExited(child)
        : child.pid === undefined ||
          !posixProcessGroupExists(child.pid, killProcess)
    if (gone) return true
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  return false
}

export function spawnProcessTree(command, args, options = {}) {
  const { platform = process.platform, spawnProcess = spawn, ...spawnOptions } =
    options
  return spawnProcess(command, args, {
    ...spawnOptions,
    detached: platform !== 'win32',
    shell: false,
    windowsHide: true,
  })
}

export function terminateWindowsTree(
  pid,
  force,
  {
    spawnProcess = spawn,
    timeoutMs = DEFAULT_TASKKILL_TIMEOUT_MS,
  } = {},
) {
  return new Promise((resolveTermination, rejectTermination) => {
    const args = ['/PID', String(pid), '/T']
    if (force) args.push('/F')
    const killer = spawnProcess('taskkill', args, {
      shell: false,
      stdio: 'ignore',
      windowsHide: true,
    })
    let settled = false
    let timedOut = false
    let closeTimer
    const finish = (error, result) => {
      if (settled) return
      settled = true
      clearTimeout(timeoutTimer)
      clearTimeout(closeTimer)
      if (error === undefined) resolveTermination(result)
      else rejectTermination(error)
    }
    killer.once('error', (error) => finish(error))
    killer.once('close', (status, signal) => {
      if (timedOut) {
        finish(
          new Error(
            `taskkill ${args.join(' ')} timed out after ${String(timeoutMs)}ms`,
          ),
        )
        return
      }
      finish(undefined, { signal, status })
    })
    const timeoutTimer = setTimeout(() => {
      timedOut = true
      try {
        killer.kill('SIGKILL')
      } catch {
        // The bounded close wait below preserves the timeout failure.
      }
      closeTimer = setTimeout(
        () =>
          finish(
            new Error(
              `taskkill ${args.join(' ')} timed out after ${String(timeoutMs)}ms`,
            ),
          ),
        Math.min(Math.max(timeoutMs, 1), 1_000),
      )
    }, Math.max(timeoutMs, 1))
  })
}

function collectWindowsProcessRows(spawnProcess, timeoutMs) {
  return new Promise((resolveRows, rejectRows) => {
    const command = [
      "$ErrorActionPreference='Stop'",
      "@(Get-CimInstance Win32_Process | ForEach-Object { [pscustomobject]@{ ProcessId = [int]$_.ProcessId; ParentProcessId = [int]$_.ParentProcessId; CreationDate = $_.CreationDate.ToUniversalTime().ToString('o') } }) | ConvertTo-Json -Compress",
    ].join('; ')
    const child = spawnProcess(
      'powershell.exe',
      ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', command],
      {
        shell: false,
        stdio: ['ignore', 'pipe', 'pipe'],
        windowsHide: true,
      },
    )
    const stdout = []
    const stderr = []
    let bytes = 0
    let settled = false
    const finish = (error, rows) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      if (error === undefined) resolveRows(rows)
      else rejectRows(error)
    }
    const capture = (chunks, chunk) => {
      const data = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)
      bytes += data.length
      if (bytes > 1024 * 1024) {
        child.kill('SIGKILL')
        finish(new Error('Windows process snapshot exceeded output limit'))
        return
      }
      chunks.push(data)
    }
    child.stdout.on('data', (chunk) => capture(stdout, chunk))
    child.stderr.on('data', (chunk) => capture(stderr, chunk))
    child.once('error', (error) => finish(error))
    child.once('close', (status) => {
      if (status !== 0) {
        finish(
          new Error(
            `Windows process snapshot exited ${String(status)}: ${Buffer.concat(stderr).toString('utf8').trim()}`,
          ),
        )
        return
      }
      try {
        const parsed = JSON.parse(Buffer.concat(stdout).toString('utf8'))
        finish(undefined, Array.isArray(parsed) ? parsed : [parsed])
      } catch (error) {
        finish(error)
      }
    })
    const timer = setTimeout(() => {
      child.kill('SIGKILL')
      finish(new Error('Windows process snapshot timed out'))
    }, Math.max(timeoutMs, 1))
  })
}

function normalizeWindowsProcessRows(rows) {
  return rows.map((row) => {
    const pid = Number(row?.ProcessId ?? row?.pid)
    const parentPid = Number(row?.ParentProcessId ?? row?.parentPid)
    const creationDate = String(row?.CreationDate ?? row?.creationDate ?? '')
    if (
      !Number.isSafeInteger(pid) ||
      pid <= 0 ||
      !Number.isSafeInteger(parentPid) ||
      creationDate.length === 0
    ) {
      throw new Error('Windows process snapshot contains invalid process identities')
    }
    return { creationDate, parentPid, pid }
  })
}

export async function queryWindowsProcesses({
  spawnProcess = spawn,
  timeoutMs = WINDOWS_SNAPSHOT_TIMEOUT_MS,
} = {}) {
  return normalizeWindowsProcessRows(
    await collectWindowsProcessRows(spawnProcess, timeoutMs),
  )
}

function sameWindowsIdentity(left, right) {
  return (
    left !== undefined &&
    right !== undefined &&
    left.pid === right.pid &&
    left.creationDate === right.creationDate
  )
}

function windowsRowsByPid(rows) {
  return new Map(rows.map((row) => [row.pid, row]))
}

function expandWindowsTree(retained, rows) {
  const current = windowsRowsByPid(rows)
  let changed = true
  while (changed) {
    changed = false
    for (const row of rows) {
      if (retained.has(row.pid)) continue
      const parent = retained.get(row.parentPid)
      if (parent === undefined) continue
      const currentParent = current.get(parent.pid)
      if (
        currentParent !== undefined &&
        !sameWindowsIdentity(parent, currentParent)
      ) {
        continue
      }
      retained.set(row.pid, row)
      changed = true
    }
  }
}

export async function snapshotWindowsProcessTree(
  rootPid,
  {
    queryWindowsProcesses: queryProcesses = queryWindowsProcesses,
    spawnProcess = spawn,
    timeoutMs = WINDOWS_SNAPSHOT_TIMEOUT_MS,
  } = {},
) {
  const rows = normalizeWindowsProcessRows(
    await queryProcesses({ spawnProcess, timeoutMs }),
  )
  const root = rows.find((row) => row.pid === rootPid)
  if (root === undefined) {
    throw new Error('Windows process snapshot is missing the root identity')
  }
  const retained = new Map([[root.pid, root]])
  expandWindowsTree(retained, rows)
  return [...retained.values()]
}

function liveWindowsIdentities(retained, rows) {
  const current = windowsRowsByPid(rows)
  return [...retained.values()].filter((identity) =>
    sameWindowsIdentity(identity, current.get(identity.pid)),
  )
}

async function queryRetainedWindowsTree(
  retained,
  deadline,
  queryProcesses,
  spawnProcess,
) {
  const rows = normalizeWindowsProcessRows(
    await queryProcesses({
      spawnProcess,
      timeoutMs: Math.max(1, deadline - Date.now()),
    }),
  )
  expandWindowsTree(retained, rows)
  return { live: liveWindowsIdentities(retained, rows), rows }
}

async function waitForWindowsTree(
  retained,
  waitDeadline,
  queryDeadline,
  queryProcesses,
  spawnProcess,
) {
  let state
  let emptySamples = 0
  do {
    state = await queryRetainedWindowsTree(
      retained,
      queryDeadline,
      queryProcesses,
      spawnProcess,
    )
    if (state.live.length === 0) {
      emptySamples += 1
      if (emptySamples >= 2 || Date.now() >= queryDeadline) return state
      await new Promise((resolveWait) =>
        setTimeout(
          resolveWait,
          Math.min(20, Math.max(1, queryDeadline - Date.now())),
        ),
      )
      continue
    }
    emptySamples = 0
    if (Date.now() >= waitDeadline) return state
    await new Promise((resolveWait) =>
      setTimeout(
        resolveWait,
        Math.min(50, Math.max(1, waitDeadline - Date.now())),
      ),
    )
  } while (Date.now() < queryDeadline)
  return state
}

async function terminateWindowsProcessTree(
  child,
  {
    closeWatchdogMs,
    label,
    queryWindowsProcesses: queryProcesses,
    snapshotWindowsTree,
    spawnProcess,
    terminationGraceMs,
  },
) {
  const cleanupDeadline =
    Date.now() + terminationGraceMs + closeWatchdogMs
  let retained
  const taskkillFailures = []
  try {
    const identities = normalizeWindowsProcessRows(
      await snapshotWindowsTree(child.pid, {
        queryWindowsProcesses: queryProcesses,
        spawnProcess,
        timeoutMs: Math.max(1, cleanupDeadline - Date.now()),
      }),
    )
    retained = new Map(identities.map((identity) => [identity.pid, identity]))
  } catch (error) {
    try {
      await terminateWindowsTree(child.pid, true, {
        spawnProcess,
        timeoutMs: Math.max(1, cleanupDeadline - Date.now()),
      })
    } catch {
      // Preserve the snapshot failure, which means cleanup cannot be proven.
    }
    throw new Error(`${label} could not snapshot its Windows process tree`, {
      cause: error,
    })
  }

  try {
    const result = await terminateWindowsTree(child.pid, false, {
      spawnProcess,
      timeoutMs: Math.max(1, cleanupDeadline - Date.now()),
    })
    if (result.status !== 0) {
      taskkillFailures.push(`status ${String(result.status)}`)
    }
  } catch (error) {
    taskkillFailures.push(error instanceof Error ? error.message : String(error))
  }
  const graceDeadline = Math.min(
    cleanupDeadline,
    Date.now() + terminationGraceMs,
  )
  let state = await waitForWindowsTree(
    retained,
    graceDeadline,
    cleanupDeadline,
    queryProcesses,
    spawnProcess,
  )
  if (state.live.length === 0) return

  const forceDeadline = cleanupDeadline
  while (state.live.length > 0 && Date.now() < forceDeadline) {
    for (const identity of [...state.live].reverse()) {
      const current = await queryRetainedWindowsTree(
        retained,
        forceDeadline,
        queryProcesses,
        spawnProcess,
      )
      const stillLive = current.live.find((item) =>
        sameWindowsIdentity(item, identity),
      )
      if (stillLive === undefined) continue
      try {
        const result = await terminateWindowsTree(identity.pid, true, {
          spawnProcess,
          timeoutMs: Math.max(1, forceDeadline - Date.now()),
        })
        if (result.status !== 0) {
          taskkillFailures.push(`status ${String(result.status)}`)
        }
      } catch (error) {
        taskkillFailures.push(
          error instanceof Error ? error.message : String(error),
        )
      }
    }
    state = await waitForWindowsTree(
      retained,
      Math.min(forceDeadline, Date.now() + 100),
      forceDeadline,
      queryProcesses,
      spawnProcess,
    )
  }
  if (state.live.length === 0) return

  const survivors = state.live.map((identity) => identity.pid)
  const failureDetail = taskkillFailures.length === 0
    ? ''
    : `taskkill failed with ${taskkillFailures.join(', ')}; `
  throw new Error(
    `${label} ${failureDetail}did not exit after forced termination (PIDs: ${survivors.join(', ')})`,
  )
}

async function signalProcessTree(
  child,
  signal,
  { killProcess = process.kill, platform = process.platform, spawnProcess = spawn },
) {
  if (child.pid === undefined) return
  if (platform === 'win32') {
    await terminateWindowsTree(child.pid, signal === 'SIGKILL', {
      spawnProcess,
    })
    return
  }
  try {
    killProcess(-child.pid, signal)
  } catch (error) {
    if (error?.code === 'ESRCH') return
    child.kill(signal)
  }
}

export async function terminateProcessTree(
  child,
  {
    closeWatchdogMs = DEFAULT_CLOSE_WATCHDOG_MS,
    killProcess = process.kill,
    label = 'process tree',
    platform = process.platform,
    queryWindowsProcesses: queryProcesses = queryWindowsProcesses,
    snapshotWindowsTree = snapshotWindowsProcessTree,
    spawnProcess = spawn,
    terminationGraceMs = DEFAULT_TERMINATION_GRACE_MS,
  } = {},
) {
  if (child.pid === undefined) return
  if (platform === 'win32') {
    await terminateWindowsProcessTree(child, {
      closeWatchdogMs,
      label,
      queryWindowsProcesses: queryProcesses,
      snapshotWindowsTree,
      spawnProcess,
      terminationGraceMs,
    })
    return
  }
  if (!posixProcessGroupExists(child.pid, killProcess)) {
    return
  }
  const signalOptions = { killProcess, platform, spawnProcess }
  await signalProcessTree(child, 'SIGTERM', signalOptions)
  if (
    await treeExitsWithin(
      child,
      terminationGraceMs,
      platform,
      killProcess,
    )
  ) {
    return
  }
  await signalProcessTree(child, 'SIGKILL', signalOptions)
  if (
    await treeExitsWithin(child, closeWatchdogMs, platform, killProcess)
  ) {
    return
  }
  throw new Error(`${label} did not exit after forced termination`)
}

export function runBoundedCommand(
  command,
  args,
  {
    closeWatchdogMs = DEFAULT_CLOSE_WATCHDOG_MS,
    cwd,
    env = process.env,
    label = command,
    maxBuffer = 1024 * 1024,
    platform = process.platform,
    spawnProcess = spawn,
    terminationGraceMs = DEFAULT_TERMINATION_GRACE_MS,
    timeoutMs,
  },
) {
  return new Promise((resolveCommand, rejectCommand) => {
    let child
    try {
      child = spawnProcessTree(command, args, {
        cwd,
        env,
        platform,
        spawnProcess,
        stdio: ['ignore', 'pipe', 'pipe'],
      })
    } catch (error) {
      rejectCommand(error)
      return
    }

    const stdout = []
    const stderr = []
    let outputBytes = 0
    let terminationReason
    let settled = false

    const timeoutTimer = setTimeout(() => {
      void beginTermination('timeout')
    }, timeoutMs)

    function finish(error, result) {
      if (settled) return
      settled = true
      clearTimeout(timeoutTimer)
      if (error === undefined) resolveCommand(result)
      else rejectCommand(error)
    }

    async function beginTermination(reason) {
      if (terminationReason !== undefined || settled) return
      terminationReason = reason
      clearTimeout(timeoutTimer)
      try {
        await terminateProcessTree(child, {
          closeWatchdogMs,
          label,
          platform,
          spawnProcess,
          terminationGraceMs,
        })
      } catch (error) {
        finish(error)
        return
      }
      finish(
        new Error(
          reason === 'timeout'
            ? `${label} timed out after ${String(timeoutMs)}ms`
            : `${label} exceeded output limit`,
        ),
      )
    }

    function capture(chunks, chunk) {
      if (terminationReason !== undefined) return
      const data = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)
      outputBytes += data.length
      if (outputBytes > maxBuffer) {
        void beginTermination('output')
        return
      }
      chunks.push(data)
    }

    child.stdout.on('data', (chunk) => capture(stdout, chunk))
    child.stderr.on('data', (chunk) => capture(stderr, chunk))
    child.once('error', (error) => {
      if (terminationReason === undefined) finish(error)
    })
    child.once('close', (status, signal) => {
      if (terminationReason !== undefined) return
      finish(undefined, {
        signal,
        status,
        stderr: Buffer.concat(stderr).toString('utf8'),
        stdout: Buffer.concat(stdout).toString('utf8'),
      })
    })
  })
}
