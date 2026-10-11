import { execFile } from 'node:child_process'
import { access, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import type { IncomingMessage, ServerResponse } from 'node:http'
import { promisify } from 'node:util'
import type { Plugin } from 'vite'

const run = promisify(execFile)
const endpoint = '/__local-preview/dictation'
const maxBytes = 8 * 1024 * 1024

export function acceptsLocalVoice(req: Pick<IncomingMessage, 'method' | 'headers'> & { socket: { remoteAddress?: string } }, port: 5174 | 5175 = 5174): boolean {
  const host = `127.0.0.1:${port}`
  const origin = `http://${host}`
  const peer = req.socket.remoteAddress
  if (peer !== '127.0.0.1' && peer !== '::ffff:127.0.0.1') return false
  if (req.headers.host !== host) return false
  if (req.method === 'GET') return true
  return req.method === 'POST' && req.headers.origin === origin && req.headers['x-anban-local-voice'] === '1'
}

function json(res: ServerResponse, code: number, body: unknown) {
  res.writeHead(code, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' })
  res.end(JSON.stringify(body))
}

export function localDictation(port: 5174 | 5175 = 5174): Plugin {
  let busy = false
  return {
    name: 'local-preview-dictation',
    apply: 'serve',
    configureServer(server) {
      const runtime = process.env.ANBAN_PREVIEW_WHISPER_ROOT
      const python = runtime && path.join(runtime, '.venv', 'Scripts', 'python.exe')
      const model = runtime && path.join(runtime, 'models', 'base.pt')
      const script = path.resolve(server.config.root, 'scripts', 'transcribe-local.py')
      const scratch = path.resolve(server.config.root, 'tmp', 'voice-input')
      server.middlewares.use(async (req, res, next) => {
        if (req.url !== endpoint) { next(); return }
        if (!acceptsLocalVoice(req, port)) { json(res, 403, { error: '仅允许本机预览页面调用。' }); return }
        if (!runtime || !python || !model) { json(res, 503, { error: '本机转写环境尚未启用。' }); return }
        try { await Promise.all([access(python), access(model), access(script), access(path.join(runtime, 'bin', 'ffmpeg.exe'))]) } catch {
          json(res, 503, { error: '缺少本机转写程序或已下载模型。' }); return
        }
        if (req.method === 'GET') { json(res, 200, { available: true, mode: 'local', maxSeconds: 60 }); return }
        if (busy) { json(res, 429, { error: '正在处理上一段语音，请稍后再试。' }); return }
        if (!/^audio\/(webm|ogg|mp4|wav)(;|$)/i.test(req.headers['content-type'] ?? '')) { json(res, 415, { error: '不支持这种录音格式。' }); return }
        if (Number(req.headers['content-length']) > maxBytes) { json(res, 413, { error: '录音过大，请缩短后重试。' }); return }
        busy = true
        let folder: string | undefined
        let uploadTimer: ReturnType<typeof setTimeout> | undefined
        try {
          uploadTimer = setTimeout(() => req.destroy(), 15_000)
          const chunks: Buffer[] = []
          let size = 0
          for await (const chunk of req) {
            const buffer = Buffer.from(chunk)
            size += buffer.length
            if (size > maxBytes) { json(res, 413, { error: '录音过大，请缩短后重试。' }); return }
            chunks.push(buffer)
          }
          clearTimeout(uploadTimer)
          if (!size) { json(res, 400, { error: '没有收到录音，请重新试一次。' }); return }
          await mkdir(scratch, { recursive: true })
          folder = await mkdtemp(path.join(scratch, 'clip-'))
          const audio = path.join(folder, 'input.audio')
          const result = path.join(folder, 'result.json')
          await writeFile(audio, Buffer.concat(chunks))
          // No shell, downloads or arbitrary user paths; one short-lived CPU process.
          await run(python, ['-X', 'utf8', script, runtime, audio, result], { timeout: 120_000, windowsHide: true, maxBuffer: 1024 * 1024 })
          const output: unknown = JSON.parse(await readFile(result, 'utf8'))
          if (!output || typeof output !== 'object' || !('text' in output) || typeof output.text !== 'string') throw new Error('invalid result')
          json(res, 200, { text: output.text })
        } catch {
          if (!res.writableEnded && !res.destroyed) json(res, 503, { error: '本机转写未完成，请用较短的一段话重试。原有文字已保留。' })
        } finally {
          clearTimeout(uploadTimer)
          // Only remove the unique temporary folder created for this request.
          if (folder && path.dirname(folder) === scratch) await rm(folder, { recursive: true, force: true }).catch(() => {})
          busy = false
        }
      })
    },
  }
}
