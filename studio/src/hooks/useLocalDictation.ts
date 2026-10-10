import { useEffect, useRef, useState } from 'react'
import { getApiErrorMessage } from '@/lib/http-client'

interface Recording {
  stream?: MediaStream
  recorder?: MediaRecorder
  timer?: ReturnType<typeof setTimeout>
  request?: AbortController
}

/** Explicit recording; injected production transport or opt-in local preview. Never auto-sends text. */
export function useLocalDictation(onText: (text: string) => void, transcribe?: (audio: Blob, signal: AbortSignal) => Promise<string>) {
  const [state, setState] = useState<'idle' | 'starting' | 'listening' | 'stopping'>('idle')
  const [notice, setNotice] = useState('')
  const current = useRef<Recording | null>(null)
  const onTextRef = useRef(onText)
  useEffect(() => { onTextRef.current = onText }, [onText])
  const supported = typeof MediaRecorder !== 'undefined' && !!navigator.mediaDevices?.getUserMedia

  function release() {
    const session = current.current
    current.current = null
    if (!session) return
    clearTimeout(session.timer)
    session.request?.abort()
    if (session.recorder) {
      session.recorder.ondataavailable = session.recorder.onstop = session.recorder.onerror = null
      if (session.recorder.state !== 'inactive') { try { session.recorder.stop() } catch { /* already stopped */ } }
    }
    session.stream?.getTracks().forEach(track => track.stop())
  }
  useEffect(() => () => release(), [])

  async function start(base: string) {
    if (current.current) return
    if (!supported) { setNotice('当前浏览器无法录音，可以使用系统或输入法的语音输入。'); return }
    const session: Recording = {}
    current.current = session
    setNotice(''); setState('starting')
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      if (current.current !== session) { stream.getTracks().forEach(track => track.stop()); return }
      session.stream = stream
      const mimeType = ['audio/webm;codecs=opus', 'audio/ogg;codecs=opus', 'audio/mp4'].find(type => MediaRecorder.isTypeSupported(type))
      const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined)
      session.recorder = recorder
      const chunks: Blob[] = []
      let size = 0
      recorder.ondataavailable = event => {
        if (current.current !== session || !event.data.size) return
        size += event.data.size
        if (size > 8 * 1024 * 1024) {
          release(); setState('idle'); setNotice('录音过大，请缩短后重试。原有文字保留。'); return
        }
        chunks.push(event.data)
      }
      recorder.onerror = () => {
        if (current.current !== session) return
        release(); setState('idle'); setNotice('录音没有成功，请检查麦克风后重试。原有文字保留。')
      }
      recorder.onstop = async () => {
        if (current.current !== session) return
        clearTimeout(session.timer)
        stream.getTracks().forEach(track => track.stop())
        setState('stopping')
        const request = new AbortController()
        session.request = request
        session.timer = setTimeout(() => request.abort(), 125_000)
        try {
          const audio = new Blob(chunks, { type: recorder.mimeType || mimeType || 'audio/webm' })
          if (!audio.size) throw new Error('没有收到录音，请再试一次。')
          const result = transcribe ? { text: await transcribe(audio, request.signal) } : await (async () => {
          const response = await fetch('/__local-preview/dictation', {
            method: 'POST', headers: { 'Content-Type': audio.type, 'X-Anban-Local-Voice': '1' }, body: audio, signal: request.signal,
          })
          const result: { text?: unknown; error?: unknown } = await response.json()
          if (!response.ok) throw new Error(typeof result.error === 'string' ? result.error : '本机转写暂时不可用。')
          return result
          })()
          if (typeof result.text !== 'string') throw new Error('本机转写返回异常，请重试。')
          if (current.current !== session) return
          const text = result.text.trim()
          if (text) onTextRef.current(base + (base && !/\s$/.test(base) ? '\n' : '') + text)
          setNotice(text ? transcribe ? '已转成文字，请检查后再发送。' : '已在本机转成文字，请检查后再发送。' : '没有识别到清晰的语音，请靠近麦克风重试。')
        } catch (error) {
          if (current.current !== session) return
          setNotice(`${getApiErrorMessage(error, '语音转文字未完成，请用短句重试。')} 原有文字已保留。`)
        } finally {
          if (current.current === session) { release(); setState('idle') }
        }
      }
      recorder.start(1000)
      setState('listening')
      // Leave a margin below the server's 60-second limit.
      session.timer = setTimeout(stop, 55_000)
    } catch (error) {
      if (current.current !== session) return
      release(); setState('idle')
      setNotice(error instanceof Error && error.name === 'NotAllowedError'
        ? '麦克风权限未获允许，请允许此页面使用麦克风后重试。'
        : '无法启动录音，请检查麦克风连接和权限。')
    }
  }

  function stop() {
    const session = current.current
    if (!session || session.request) return
    if (!session.recorder) { release(); setState('idle'); setNotice('已取消录音。'); return }
    clearTimeout(session.timer)
    setState('stopping')
    session.timer = setTimeout(() => {
      if (current.current !== session || session.request) return
      release(); setState('idle'); setNotice('录音未正常结束，请重试。原有文字保留。')
    }, 4000)
    try {
      if (session.recorder.state !== 'inactive') session.recorder.stop()
      session.stream?.getTracks().forEach(track => track.stop())
    } catch { release(); setState('idle'); setNotice('录音已停止，请重试。原有文字保留。') }
  }

  return { state, notice, supported, active: state !== 'idle', start, stop }
}
