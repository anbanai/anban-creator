import { spawn } from 'node:child_process'

const DEFAULT_TERMINATION_GRACE_MS = 1_000
const DEFAULT_CLOSE_WATCHDOG_MS = 10_000

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
  return new Promise((resolveTermination) => {
    const args = ['/PID', String(pid), '/T']
    if (force) args.push('/F')
    const killer = spawnProcess('taskkill', args, {
      shell: false,
      stdio: 'ignore',
      windowsHide: true,
    })
    killer.once('error', resolveTermination)
    killer.once('close', resolveTermination)
  })
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
    spawnProcess = spawn,
    terminationGraceMs = DEFAULT_TERMINATION_GRACE_MS,
  } = {},
) {
  if (child.pid === undefined) return
  if (
    platform === 'win32'
      ? hasExited(child)
      : !posixProcessGroupExists(child.pid, killProcess)
  ) {
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
