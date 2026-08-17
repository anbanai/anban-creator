import { spawn } from 'node:child_process'
import { access, readFile, readdir, stat } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { createServer } from 'node:net'
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path'

const COMMAND_TIMEOUT_MS = 180_000
const LAUNCH_TIMEOUT_MS = 120_000
const OUTPUT_LIMIT = 1024 * 1024

function requiredPath(name) {
  const value = process.env[name]
  if (typeof value !== 'string' || !isAbsolute(value)) {
    throw new Error(`${name} must be an absolute path`)
  }
  return resolve(value)
}

async function run(command, args, options = {}) {
  return new Promise((resolveResult, reject) => {
    const child = spawn(command, args, {
      cwd: options.cwd,
      env: options.env ?? process.env,
      shell: false,
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    })
    let stdout = ''
    let stderr = ''
    let settled = false
    const finish = (error, result) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      if (error === undefined) resolveResult(result)
      else reject(error)
    }
    const append = (current, chunk) => {
      const next = current + chunk.toString('utf8')
      if (Buffer.byteLength(next, 'utf8') > OUTPUT_LIMIT) {
        child.kill()
        throw new Error('Desktop acceptance command exceeded output limit')
      }
      return next
    }
    child.stdout.on('data', (chunk) => {
      try {
        stdout = append(stdout, chunk)
      } catch (error) {
        finish(error)
      }
    })
    child.stderr.on('data', (chunk) => {
      try {
        stderr = append(stderr, chunk)
      } catch (error) {
        finish(error)
      }
    })
    child.once('error', (error) => finish(error))
    child.once('exit', (code, signal) => {
      if (code === 0) {
        finish(undefined, { stdout, stderr })
      } else {
        finish(
          new Error(
            `${options.label ?? basename(command)} exited ${String(code ?? signal ?? 1)}`,
          ),
        )
      }
    })
    const timer = setTimeout(() => {
      child.kill()
      finish(
        new Error(
          `${options.label ?? basename(command)} timed out after ${String(options.timeoutMs ?? COMMAND_TIMEOUT_MS)}ms`,
        ),
      )
    }, options.timeoutMs ?? COMMAND_TIMEOUT_MS)
  })
}

async function resolveDshBin(desktopRoot) {
  const desktopManifest = join(
    desktopRoot,
    'dsh-plugin-desktop',
    'package.json',
  )
  const requireFromDesktop = createRequire(desktopManifest)
  const manifestPath = requireFromDesktop.resolve(
    '@deepseek-ai/dsh/package.json',
  )
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
  const relativeBin =
    typeof manifest.bin === 'string' ? manifest.bin : manifest.bin?.dsh
  if (typeof relativeBin !== 'string') {
    throw new Error('@deepseek-ai/dsh does not declare its public dsh bin')
  }
  const packageRoot = dirname(manifestPath)
  const entrypoint = resolve(packageRoot, relativeBin)
  const fromPackage = relative(packageRoot, entrypoint)
  if (
    fromPackage === '' ||
    fromPackage === '..' ||
    fromPackage.startsWith(`..${sep}`) ||
    isAbsolute(fromPackage)
  ) {
    throw new Error('@deepseek-ai/dsh declares an invalid public dsh bin')
  }
  await access(entrypoint)
  return entrypoint
}

async function installPlugin() {
  const desktopRoot = requiredPath('DSH_DESKTOP_ROOT')
  const tarball = requiredPath('DSH_PLUGIN_TARBALL')
  const dshHome = requiredPath('DSH_HOME')
  if (!/^anban-dsh-plugin-\d+\.\d+\.\d+\.tgz$/.test(basename(tarball))) {
    throw new Error('DSH_PLUGIN_TARBALL is not an exact versioned plugin tarball')
  }
  const tarballStatus = await stat(tarball)
  if (!tarballStatus.isFile()) {
    throw new Error('DSH_PLUGIN_TARBALL is not a regular file')
  }
  const dshBin = await resolveDshBin(desktopRoot)
  const environment = {
    ...process.env,
    DSH_HOME: dshHome,
    DSH_TELEMETRY_DISABLED: '1',
  }
  await run(
    process.execPath,
    [dshBin, 'plugin', '--profile', 'desktop', 'add', tarball],
    {
      cwd: dirname(tarball),
      env: environment,
      label: 'public DSH plugin add',
    },
  )
  await run(
    process.execPath,
    [
      dshBin,
      'plugin',
      '--profile',
      'desktop',
      'exec',
      'anban-dsh',
      'install-presets',
    ],
    {
      cwd: dirname(tarball),
      env: environment,
      label: 'public DSH preset install',
    },
  )
  const dumped = await run(
    process.execPath,
    [dshBin, '--profile', 'desktop', '--dump-config'],
    {
      cwd: dirname(tarball),
      env: environment,
      label: 'public DSH Desktop profile dump',
    },
  )
  for (const expected of [
    '@anban/dsh-plugin/anban-mcp',
    '@anban/dsh-plugin/preset-manager',
  ]) {
    if (!dumped.stdout.includes(expected)) {
      throw new Error(`Desktop profile dump is missing ${expected}`)
    }
  }
  process.stdout.write(`Installed ${basename(tarball)} into public DSH profile desktop\n`)
}

async function findPackagedExecutable(desktopRoot) {
  const distRoot = join(desktopRoot, 'dsh-plugin-desktop', 'dist')
  const matches = []
  async function visit(directory) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) {
        await visit(path)
      } else if (
        entry.isFile() &&
        ((process.platform === 'darwin' &&
          path.endsWith(
            join('DSH Desktop.app', 'Contents', 'MacOS', 'DSH Desktop'),
          )) ||
          (process.platform === 'win32' && entry.name === 'DSH Desktop.exe'))
      ) {
        matches.push(path)
      }
    }
  }
  await visit(distRoot)
  if (matches.length !== 1) {
    throw new Error(
      `Expected one host-native packaged DSH Desktop executable, found ${String(matches.length)}`,
    )
  }
  return matches[0]
}

async function reservePort() {
  return new Promise((resolvePort, reject) => {
    const server = createServer()
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      const port = typeof address === 'object' && address !== null
        ? address.port
        : undefined
      server.close((error) => {
        if (error !== undefined) reject(error)
        else if (port === undefined) reject(new Error('Unable to reserve a CDP port'))
        else resolvePort(port)
      })
    })
  })
}

async function waitForPackagedWindow(port, child, diagnostics) {
  const deadline = Date.now() + LAUNCH_TIMEOUT_MS
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(
        `Packaged DSH Desktop exited before opening a window: ${diagnostics()}`,
      )
    }
    try {
      const response = await fetch(`http://127.0.0.1:${String(port)}/json/list`)
      if (response.ok) {
        const targets = await response.json()
        const page = targets.find(
          (target) =>
            target.type === 'page' &&
            typeof target.url === 'string' &&
            target.url.startsWith('http://127.0.0.1:'),
        )
        if (page !== undefined) {
          const web = await fetch(page.url)
          const html = await web.text()
          if (
            web.ok &&
            html.includes('window.__DSH_BOOT__') &&
            html.toLowerCase().includes('dsh')
          ) {
            return page
          }
        }
      }
    } catch {
      // The packaged host is still starting.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250))
  }
  throw new Error('Packaged DSH Desktop did not expose its loopback window')
}

async function stopPackagedApp(child) {
  if (child.exitCode !== null || child.signalCode !== null) return
  child.kill()
  const exited = await Promise.race([
    new Promise((resolveExit) => child.once('exit', () => resolveExit(true))),
    new Promise((resolveExit) => setTimeout(() => resolveExit(false), 10_000)),
  ])
  if (!exited && child.exitCode === null && child.signalCode === null) {
    child.kill('SIGKILL')
  }
}

async function launchPackagedApp() {
  const desktopRoot = requiredPath('DSH_DESKTOP_ROOT')
  const dshHome = requiredPath('DSH_HOME')
  const userData = requiredPath('DSH_DESKTOP_STATE_DIR')
  const executable = await findPackagedExecutable(desktopRoot)
  const port = await reservePort()
  const child = spawn(
    executable,
    [
      `--remote-debugging-port=${String(port)}`,
      `--user-data-dir=${userData}`,
    ],
    {
      cwd: desktopRoot,
      env: {
        ...process.env,
        DSH_HOME: dshHome,
        DSH_TELEMETRY_DISABLED: '1',
      },
      shell: false,
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true,
    },
  )
  let output = ''
  const capture = (chunk) => {
    output = `${output}${chunk.toString('utf8')}`.slice(-16_384)
  }
  child.stdout.on('data', capture)
  child.stderr.on('data', capture)
  try {
    const page = await waitForPackagedWindow(port, child, () => output.trim())
    process.stdout.write(
      `Packaged DSH Desktop opened ${page.url} from ${executable}\n`,
    )
  } finally {
    await stopPackagedApp(child)
  }
}

const action = process.argv[2]
if (action === 'install') {
  await installPlugin()
} else if (action === 'launch') {
  await launchPackagedApp()
} else {
  throw new Error('usage: dsh-desktop-acceptance.mjs <install|launch>')
}
