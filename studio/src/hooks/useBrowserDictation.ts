import { useEffect, useRef, useState } from 'react'

// Web Speech is not declared by every version of lib.dom and may be prefixed.
interface Recognition {
  lang: string
  continuous: boolean
  interimResults: boolean
  onstart: (() => void) | null
  onend: (() => void) | null
  onerror: ((event: { error: string }) => void) | null
  onresult: ((event: { results: ArrayLike<ArrayLike<{ transcript: string }>> }) => void) | null
  start(): void
  stop(): void
  abort(): void
}
type RecognitionConstructor = new () => Recognition
type SpeechWindow = Window & { SpeechRecognition?: RecognitionConstructor; webkitSpeechRecognition?: RecognitionConstructor }

const unavailable = '当前浏览器不支持网页语音识别。可以点击输入框，使用系统或输入法的语音输入。'
const errors: Record<string, string> = {
  'not-allowed': '麦克风权限未获允许。请允许此页面使用麦克风后再试，或使用系统语音输入。',
  'service-not-allowed': '当前浏览器未开放语音识别服务，可以使用系统语音输入。',
  'audio-capture': '没有找到可用麦克风，请检查麦克风连接和权限。',
  'network': '浏览器语音识别服务连接失败。请检查网络，或使用系统语音输入。',
  'no-speech': '没有听到清晰的语音，点击语音按钮可以重试。',
  'language-not-supported': '当前浏览器的语音服务不支持中文，可以使用系统语音输入。',
}

export function useBrowserDictation(onText: (value: string) => void) {
  const [state, setState] = useState<'idle' | 'starting' | 'listening' | 'stopping'>('idle')
  const [notice, setNotice] = useState('')
  const recognitionRef = useRef<Recognition | null>(null)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const onTextRef = useRef(onText)
  useEffect(() => { onTextRef.current = onText }, [onText])
  const Speech = typeof window === 'undefined' ? undefined : ((window as SpeechWindow).SpeechRecognition ?? (window as SpeechWindow).webkitSpeechRecognition)

  function dispose() {
    clearTimeout(timeoutRef.current)
    const recognition = recognitionRef.current
    recognitionRef.current = null
    if (!recognition) return
    recognition.onstart = recognition.onend = recognition.onerror = recognition.onresult = null
    try { recognition.abort() } catch { /* Already disconnected. */ }
  }

  useEffect(() => () => dispose(), [])

  function start(base: string) {
    if (recognitionRef.current) return
    if (!Speech) { setNotice(unavailable); return }
    setNotice('')
    let heard = false
    try {
      const recognition = new Speech()
      recognitionRef.current = recognition
      recognition.lang = 'zh-CN'
      recognition.continuous = true
      recognition.interimResults = true
      recognition.onstart = () => {
        if (recognitionRef.current === recognition) setState('listening')
      }
      recognition.onresult = event => {
        if (recognitionRef.current !== recognition) return
        // Each event contains the current session's complete result list. Replacing
        // it avoids duplicating interim text when the recognizer corrects a word.
        const transcript = Array.from(event.results, result => result[0]?.transcript ?? '').join('').trim()
        heard = !!transcript
        onTextRef.current(base + (base && transcript && !/\s$/.test(base) ? '\n' : '') + transcript)
      }
      recognition.onerror = event => {
        if (recognitionRef.current !== recognition) return
        setNotice(event.error === 'aborted' ? '语音输入已停止，已有文字保留。' : errors[event.error] ?? '语音识别暂时不可用，已有文字保留，请重试或使用系统语音输入。')
        dispose(); setState('idle')
      }
      recognition.onend = () => {
        if (recognitionRef.current !== recognition) return
        dispose(); setState('idle')
        setNotice(heard ? '语音已转成文字，请检查后再发送。' : '本次没有识别到文字，可以重新开始。')
      }
      setState('starting')
      recognition.start()
    } catch {
      dispose(); setState('idle')
      setNotice('无法启动浏览器语音识别，请检查麦克风权限，或使用系统语音输入。')
    }
  }

  function stop() {
    const recognition = recognitionRef.current
    if (!recognition) return
    if (state === 'starting') {
      dispose(); setState('idle'); setNotice('已取消启动语音输入。'); return
    }
    if (state === 'stopping') return
    setState('stopping')
    // Some embedded browsers expose the API but never finish recognition.
    timeoutRef.current = setTimeout(() => {
      if (recognitionRef.current !== recognition) return
      dispose(); setState('idle'); setNotice('语音已停止，已有文字保留，请检查后再发送。')
    }, 4000)
    try { recognition.stop() } catch {
      dispose(); setState('idle'); setNotice('语音已停止，已有文字保留。')
    }
  }

  return { supported: !!Speech, active: state !== 'idle', state, notice, start, stop }
}
