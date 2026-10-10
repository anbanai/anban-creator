import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import PortraitOnboardingPreview from './PortraitOnboardingPreview'
import { PortraitConversation } from '@/components/projects/PortraitConversation'

async function sample() {
  fireEvent.click(screen.getByRole('button', { name: '试用示例回答' }))
  fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(''))
}

describe('conversation portrait preview', () => {
  it('starts blank and grows several facets from one message without claiming confirmation', async () => {
    render(<PortraitOnboardingPreview />)
    const portrait = screen.getByRole('complementary', { name: '正在形成的 IP 画像' })
    expect(within(portrait).getByText('还没认识的你')).toBeInTheDocument()
    expect(screen.queryByText(/小林/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /确认画像/ })).not.toBeInTheDocument()
    await sample()
    expect(within(portrait).getByText('小林，一家社区花店的经营者。')).toBeInTheDocument()
    expect(within(portrait).getByText('刚开始养花，担心养不活的人。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始创作' })).toBeEnabled()
  })

  it('does not treat arbitrary input as scripted facts, and discloses the simulation', async () => {
    render(<PortraitOnboardingPreview />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我是做软件的，不是花店。' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(''))
    expect(screen.getByText('还没认识的你')).toBeInTheDocument()
    expect(screen.getByText(/这里还没有接入真实 AI/)).toBeInTheDocument()
    expect(screen.queryByText('小林 · 社区花店主')).not.toBeInTheDocument()
    await sample(); await sample(); await sample()
    expect(screen.queryByRole('button', { name: /确认画像/ })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续示例，不采用自由输入' }))
    expect(screen.getByRole('button', { name: '开始创作' })).toBeEnabled()
  })

  it('allows an optional facet to stay empty, allows immediate creation and ongoing correction', async () => {
    render(<PortraitOnboardingPreview />)
    await sample(); await sample(); await sample()
    const portrait = screen.getByRole('complementary', { name: '正在形成的 IP 画像' })
    expect(within(portrait).getByText('聊到时，再慢慢补充')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始创作' })).toBeEnabled()
    await sample()
    expect(within(portrait).getByText('想给家里添点花、没太多时间打理的上班族。')).toBeInTheDocument()
    expect(within(portrait).queryByText('刚开始养花，担心养不活的人。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始创作' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: '开始创作' }))
    expect(screen.getByText(/没有创建任务、生成正文或发布内容/)).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '第一篇创作交接示例' })).toHaveFocus()
  })

  it('requires explicit reset and preserves unsent text when reset is cancelled', async () => {
    render(<PortraitOnboardingPreview />)
    await sample()
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '还没说完' } })
    fireEvent.click(screen.getByRole('button', { name: '从空白重新体验' }))
    fireEvent.click(screen.getByRole('button', { name: '继续当前对话' }))
    expect(screen.getByRole('textbox')).toHaveValue('还没说完')
    fireEvent.click(screen.getByRole('button', { name: '从空白重新体验' }))
    fireEvent.click(screen.getByRole('button', { name: '清空并重新开始' }))
    expect(screen.getByRole('textbox')).toHaveValue('')
    expect(screen.getByText('还没认识的你')).toBeInTheDocument()
    expect(screen.queryByText('小林 · 社区花店主')).not.toBeInTheDocument()
  })
})

describe('portrait conversation input boundaries', () => {
  it('retains failed input, prevents duplicate sends, and blocks confirmation with an unsent correction', async () => {
    let reject!: (error: Error) => void
    const onSend = vi.fn(() => new Promise<void>((_, fail) => { reject = fail }))
    render(<PortraitConversation messages={[]} draft={{}} ready onSend={onSend} onCreate={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '纠正一下' } })
    screen.getAllByRole('button', { name: '开始创作' }).forEach(button => expect(button).toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    expect(onSend).toHaveBeenCalledTimes(1)
    await act(async () => reject(new Error('offline')))
    expect(screen.getByRole('textbox')).toHaveValue('纠正一下')
    expect(screen.getByRole('alert')).toHaveTextContent('文字已保留')
  })

  it('does not send while committing Chinese IME text or adding a newline', async () => {
    const onSend = vi.fn(async () => {})
    render(<PortraitConversation messages={[]} draft={{}} ready={false} onSend={onSend} onCreate={vi.fn()} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '你好' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter', isComposing: true })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter', keyCode: 229 })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter', shiftKey: true })
    expect(onSend).not.toHaveBeenCalled()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    await waitFor(() => expect(onSend).toHaveBeenCalledWith('你好'))
  })
})
