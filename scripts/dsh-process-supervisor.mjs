import { spawn } from 'node:child_process'

const DEFAULT_TERMINATION_GRACE_MS = 1_000
const DEFAULT_CLOSE_WATCHDOG_MS = 10_000
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
  { spawnProcess = spawn } = {},
) {
  return new Promise((resolveTermination, rejectTermination) => {
    const args = ['/PID', String(pid), '/T']
    if (force) args.push('/F')
    const killer = spawnProcess('taskkill', args, {
      shell: false,
      stdio: 'ignore',
      windowsHide: true,
    })
    killer.once('error', rejectTermination)
    killer.once('close', (status, signal) =>
      resolveTermination({ signal, status }),
    )
  })
}

function windowsPidExists(pid) {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    if (error?.code === 'ESRCH') return false
    if (error?.code === 'EPERM') return true
    throw error
  }
}

function collectWindowsProcessRows(spawnProcess) {
  return new Promise((resolveRows, rejectRows) => {
    const command = [
      "$ErrorActionPreference='Stop'",
      '@(Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId) | ConvertTo-Json -Compress',
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
    }, WINDOWS_SNAPSHOT_TIMEOUT_MS)
  })
}

export async function snapshotWindowsProcessTree(
  rootPid,
  { spawnProcess = spawn } = {},
) {
  const rows = await collectWindowsProcessRows(spawnProcess)
  const children = new Map()
  for (const row of rows) {
    const pid = Number(row?.ProcessId)
    const parentPid = Number(row?.ParentProcessId)
    if (!Number.isSafeInteger(pid) || pid <= 0 || !Number.isSafeInteger(parentPid)) {
      throw new Error('Windows process snapshot contains invalid process identities')
    }
    const siblings = children.get(parentPid) ?? []
    siblings.push(pid)
    children.set(parentPid, siblings)
  }
  const retained = [rootPid]
  for (let index = 0; index < retained.length; index += 1) {
    retained.push(...(children.get(retained[index]) ?? []))
  }
  return retained
}

async function windowsPidsExitWithin(pids, timeoutMs, processExists) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (pids.every((pid) => !processExists(pid))) return true
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  return pids.every((pid) => !processExists(pid))
}

async function terminateWindowsProcessTree(
  child,
  {
    closeWatchdogMs,
    label,
    snapshotWindowsTree,
    spawnProcess,
    terminationGraceMs,
    windowsProcessExists,
  },
) {
  let retained
  const taskkillFailures = []
  try {
    retained = [
      ...new Set(
        await snapshotWindowsTree(child.pid, {
          spawnProcess,
        }),
      ),
    ]
  } catch (error) {
    try {
      await terminateWindowsTree(child.pid, true, { spawnProcess })
    } catch {
      // Preserve the snapshot failure, which means cleanup cannot be proven.
    }
    throw new Error(`${label} could not snapshot its Windows process tree`, {
      cause: error,
    })
  }

  try {
    const result = await terminateWindowsTree(child.pid, false, { spawnProcess })
    if (result.status !== 0) {
      taskkillFailures.push(`status ${String(result.status)}`)
    }
  } catch (error) {
    taskkillFailures.push(error instanceof Error ? error.message : String(error))
  }
  if (
    await windowsPidsExitWithin(
      retained,
      terminationGraceMs,
      windowsProcessExists,
    )
  ) {
    return
  }

  const remaining = retained.filter(windowsProcessExists).reverse()
  for (const pid of remaining) {
    try {
      const result = await terminateWindowsTree(pid, true, { spawnProcess })
      if (result.status !== 0) {
        taskkillFailures.push(`status ${String(result.status)}`)
      }
    } catch (error) {
      taskkillFailures.push(error instanceof Error ? error.message : String(error))
      // The final identity check below decides whether cleanup succeeded.
    }
  }
  if (
    await windowsPidsExitWithin(retained, closeWatchdogMs, windowsProcessExists)
  ) {
    return
  }
  const survivors = retained.filter(windowsProcessExists)
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
    snapshotWindowsTree = snapshotWindowsProcessTree,
    spawnProcess = spawn,
    terminationGraceMs = DEFAULT_TERMINATION_GRACE_MS,
    windowsProcessExists = windowsPidExists,
  } = {},
) {
  if (child.pid === undefined) return
  if (platform === 'win32') {
    await terminateWindowsProcessTree(child, {
      closeWatchdogMs,
      label,
      snapshotWindowsTree,
      spawnProcess,
      terminationGraceMs,
      windowsProcessExists,
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
