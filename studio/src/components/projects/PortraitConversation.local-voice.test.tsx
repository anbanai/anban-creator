import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PortraitConversation } from './PortraitConversation'

class Recorder {
  static latest: Recorder
  static isTypeSupported = () => true
  state = 'inactive'
  mimeType = 'audio/webm'
  onstop: (() => void) | null = null
  onerror: (() => void) | null = null
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  constructor() { Recorder.latest = this }
  start() { this.state = 'recording' }
  stop() {
    this.state = 'inactive'
    this.ondataavailable?.({ data: new Blob(['audio'], { type: this.mimeType }) })
    this.onstop?.()
  }
}

const trackStop = vi.fn()
const stream = { getTracks: () => [{ stop: trackStop }] }
let getUserMedia: ReturnType<typeof vi.fn>
let fetchMock: ReturnType<typeof vi.fn>
let originalDevices: PropertyDescriptor | undefined

function setup() {
  const onSend = vi.fn(async () => {})
  const result = render(<PortraitConversation messages={[]} draft={{}} ready localVoice onSend={onSend} onCreate={vi.fn()} />)
  return { ...result, onSend }
}

describe('local voice input', () => {
  beforeEach(() => {
    trackStop.mockClear()
    originalDevices = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices')
    getUserMedia = vi.fn(async () => stream)
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
    vi.stubGlobal('MediaRecorder', Recorder)
    fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({ text: '语音文字' }) }))
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    if (originalDevices) Object.defineProperty(navigator, 'mediaDevices', originalDevices)
    else Reflect.deleteProperty(navigator, 'mediaDevices')
  })

  it('records only on click, stops the microphone before transcription, appends text and does not send', async () => {
    const { onSend } = setup()
    expect(getUserMedia).not.toHaveBeenCalled()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '已有内容' } })
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在录音'))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    expect(trackStop).toHaveBeenCalled()
    await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue('已有内容\n语音文字'))
    expect(fetchMock).toHaveBeenCalledWith('/__local-preview/dictation', expect.objectContaining({ method: 'POST', headers: { 'Content-Type': 'audio/webm', 'X-Anban-Local-Voice': '1' } }))
    expect(onSend).not.toHaveBeenCalled()
  })

  it('uses configured Server speech transport without calling the local preview and awaits user review', async () => {
    const transcribe = vi.fn(async () => '服务器识别文字')
    const onSend = vi.fn(async () => {})
    render(<PortraitConversation messages={[]} draft={{}} ready transcribe={transcribe} onSend={onSend} onCreate={vi.fn()} />)
    expect(screen.getByRole('status')).toHaveTextContent('音频会交由语音服务识别')
    expect(getUserMedia).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在录音'))
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue('服务器识别文字'))
    expect(trackStop).toHaveBeenCalled()
    expect(transcribe).toHaveBeenCalledWith(expect.any(Blob), expect.any(AbortSignal))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(onSend).not.toHaveBeenCalled()
  })

  it('releases a permission request that completes after cancellation', async () => {
    let grant!: (value: unknown) => void
    getUserMedia.mockImplementation(() => new Promise(resolve => { grant = resolve }))
    setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    await act(async () => grant(stream))
    expect(trackStop).toHaveBeenCalledOnce()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByRole('status')).toHaveTextContent('已取消录音')
  })

  it('keeps original text on transcription failure and never falls back to a remote recognizer', async () => {
    fetchMock.mockResolvedValue({ ok: false, json: async () => ({ error: '本机服务未启动' }) })
    setup()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '保留我' } })
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在录音'))
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('本机服务未启动'))
    expect(screen.getByRole('textbox')).toHaveValue('保留我')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('releases the microphone on unmount without uploading the recording', async () => {
    const { unmount } = setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在录音'))
    unmount()
    expect(trackStop).toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(Recorder.latest.onstop).toBeNull()
  })

  it('does not replace input when a stale transcription finishes after unmount', async () => {
    let complete!: (value: unknown) => void
    fetchMock.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    const { unmount, onSend } = setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('正在录音'))
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    const signal = fetchMock.mock.calls[0][1].signal as AbortSignal
    unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => complete({ ok: true, json: async () => ({ text: '迟到结果' }) }))
    expect(onSend).not.toHaveBeenCalled()
  })
})
