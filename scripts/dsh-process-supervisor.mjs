import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const DEFAULT_TERMINATION_GRACE_MS = 1_000
const DEFAULT_CLOSE_WATCHDOG_MS = 10_000
const WINDOWS_JOB_SCRIPT = fileURLToPath(
  new URL('./dsh-windows-job.ps1', import.meta.url),
)

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

async function posixTreeExitsWithin(pid, timeoutMs, killProcess) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (!posixProcessGroupExists(pid, killProcess)) return true
    await new Promise((resolveWait) => setTimeout(resolveWait, 20))
  }
  return false
}

function windowsJobArguments(command, args) {
  const payload = Buffer.from(
    JSON.stringify({ args, command }),
    'utf8',
  ).toString('base64')
  return [
    '-NoLogo',
    '-NoProfile',
    '-NonInteractive',
    '-ExecutionPolicy',
    'Bypass',
    '-File',
    WINDOWS_JOB_SCRIPT,
    '-Payload',
    payload,
  ]
}

export function spawnProcessTree(command, args, options = {}) {
  const { platform = process.platform, spawnProcess = spawn, ...spawnOptions } =
    options
  const wrappedCommand = platform === 'win32' ? 'powershell.exe' : command
  const wrappedArgs = platform === 'win32'
    ? windowsJobArguments(command, args)
    : args
  return spawnProcess(wrappedCommand, wrappedArgs, {
    ...spawnOptions,
    detached: platform !== 'win32',
    shell: false,
    windowsHide: true,
  })
}

function terminateWindowsJob(child, { label, timeoutMs }) {
  if (hasExited(child)) return Promise.resolve()
  return new Promise((resolveTermination, rejectTermination) => {
    let settled = false
    let timer
    const finish = (error) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      child.off('close', onClose)
      child.off('error', onError)
      if (error === undefined) resolveTermination()
      else rejectTermination(error)
    }
    const onClose = () => finish()
    const onError = (error) => finish(error)
    child.once('close', onClose)
    child.once('error', onError)
    timer = setTimeout(
      () => finish(new Error(`${label} Windows Job Object did not close`)),
      Math.max(1, timeoutMs),
    )
    try {
      child.kill('SIGKILL')
    } catch (error) {
      finish(error)
    }
  })
}

function signalPosixProcessTree(child, signal, killProcess) {
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
    terminationGraceMs = DEFAULT_TERMINATION_GRACE_MS,
  } = {},
) {
  if (child.pid === undefined) return
  if (platform === 'win32') {
    await terminateWindowsJob(child, {
      label,
      timeoutMs: terminationGraceMs + closeWatchdogMs,
    })
    return
  }
  if (!posixProcessGroupExists(child.pid, killProcess)) return

  signalPosixProcessTree(child, 'SIGTERM', killProcess)
  if (
    await posixTreeExitsWithin(child.pid, terminationGraceMs, killProcess)
  ) {
    return
  }
  signalPosixProcessTree(child, 'SIGKILL', killProcess)
  if (await posixTreeExitsWithin(child.pid, closeWatchdogMs, killProcess)) {
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
