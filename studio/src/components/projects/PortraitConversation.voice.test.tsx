import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PortraitConversation } from './PortraitConversation'

class FakeRecognition {
  static latest: FakeRecognition
  lang = ''
  continuous = false
  interimResults = false
  onstart: (() => void) | null = null
  onend: (() => void) | null = null
  onerror: ((event: { error: string }) => void) | null = null
  onresult: ((event: { results: { transcript: string }[][] }) => void) | null = null
  start = vi.fn()
  stop = vi.fn()
  abort = vi.fn()
  constructor() { FakeRecognition.latest = this }
}

function setup() {
  const send = vi.fn(async () => {})
  const rendered = render(<PortraitConversation messages={[]} draft={{}} ready onSend={send} onCreate={vi.fn()} />)
  return { ...rendered, send }
}

describe('portrait voice input', () => {
  beforeEach(() => {
    vi.stubGlobal('SpeechRecognition', FakeRecognition)
    vi.stubGlobal('webkitSpeechRecognition', undefined)
  })
  afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

  it('appends dictation without duplicating interim revisions and never sends it automatically', () => {
    const { send } = setup()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '原有文字' } })
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    const speech = FakeRecognition.latest
    expect(speech.lang).toBe('zh-CN')
    expect(speech.start).toHaveBeenCalledOnce()
    act(() => speech.onstart?.())
    expect(screen.getByRole('textbox')).toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    screen.getAllByRole('button', { name: '开始创作' }).forEach(button => expect(button).toBeDisabled())
    act(() => speech.onresult?.({ results: [[{ transcript: '我卖' }]] }))
    act(() => speech.onresult?.({ results: [[{ transcript: '我卖花。' }], [{ transcript: '面向新手。' }]] }))
    expect(screen.getByRole('textbox')).toHaveValue('原有文字\n我卖花。面向新手。')
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    expect(speech.stop).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: '发送消息' })).toBeDisabled()
    act(() => speech.onend?.())
    expect(screen.getByRole('textbox')).not.toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled()
    expect(send).not.toHaveBeenCalled()
  })

  it('explains unsupported browsers while keeping text input available', () => {
    vi.stubGlobal('SpeechRecognition', undefined)
    setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    expect(screen.getByRole('status')).toHaveTextContent('当前浏览器不支持')
    expect(screen.getByRole('textbox')).toHaveFocus()
    expect(screen.getByRole('textbox')).not.toHaveAttribute('readonly')
  })

  it('supports the prefixed implementation and preserves text after a denied permission', () => {
    vi.stubGlobal('SpeechRecognition', undefined)
    vi.stubGlobal('webkitSpeechRecognition', FakeRecognition)
    setup()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '别丢掉' } })
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    act(() => FakeRecognition.latest.onerror?.({ error: 'not-allowed' }))
    expect(screen.getByRole('status')).toHaveTextContent('麦克风权限未获允许')
    expect(screen.getByRole('textbox')).toHaveValue('别丢掉')
    expect(screen.getByRole('button', { name: '开始语音输入' })).toBeEnabled()
    expect(FakeRecognition.latest.abort).toHaveBeenCalledOnce()
  })

  it('cancels pending microphone startup and ignores late results after unmount', () => {
    const { unmount } = setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    const first = FakeRecognition.latest
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    expect(first.abort).toHaveBeenCalledOnce()
    expect(first.onresult).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    const second = FakeRecognition.latest
    unmount()
    expect(second.abort).toHaveBeenCalledOnce()
    expect(second.onresult).toBeNull()
  })

  it('releases a hung recognizer after stopping and preserves the captured text', () => {
    vi.useFakeTimers()
    setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    act(() => FakeRecognition.latest.onstart?.())
    act(() => FakeRecognition.latest.onresult?.({ results: [[{ transcript: '保留这句话' }]] }))
    fireEvent.click(screen.getByRole('button', { name: '停止语音输入' }))
    act(() => vi.advanceTimersByTime(4000))
    expect(screen.getByRole('textbox')).toHaveValue('保留这句话')
    expect(screen.getByRole('button', { name: '开始语音输入' })).toBeEnabled()
    expect(FakeRecognition.latest.abort).toHaveBeenCalledOnce()
  })

  it('recovers if the browser exposes the API but construction fails', () => {
    vi.stubGlobal('SpeechRecognition', class { constructor() { throw new Error('unavailable') } })
    setup()
    fireEvent.click(screen.getByRole('button', { name: '开始语音输入' }))
    expect(screen.getByRole('status')).toHaveTextContent('无法启动浏览器语音识别')
    expect(screen.getByRole('textbox')).not.toHaveAttribute('readonly')
  })
})
