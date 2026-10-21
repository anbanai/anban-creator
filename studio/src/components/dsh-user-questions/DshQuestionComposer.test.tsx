import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { DshQuestionComposer, type DshQuestion } from './DshQuestionComposer'

const questions: DshQuestion[] = [
  {
    id: 'platform',
    header: '平台',
    question: '主要发布在哪个平台？',
    detail: '**选择一个主平台**',
    options: [
      { label: '微信公众号 (推荐)', description: '适合深度内容' },
      { label: '小红书' },
    ],
  },
  {
    id: 'tone',
    question: '希望采用什么风格？',
    multiSelect: true,
    options: [{ label: '克制' }, { label: '轻松' }],
  },
]

describe('DshQuestionComposer', () => {
  it('renders one DSH-style question at a time and navigates the batch', () => {
    render(<DshQuestionComposer questions={questions} onSubmit={vi.fn()} />)

    expect(screen.getByRole('heading', { name: '主要发布在哪个平台？' })).toBeInTheDocument()
    expect(screen.getByText(/^微信公众号/)).toBeInTheDocument()
    expect(screen.getByText('推荐')).toBeInTheDocument()
    expect(screen.getByText('选择一个主平台')).toBeInTheDocument()
    expect(screen.queryByText('希望采用什么风格？')).not.toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: '下一题' })[0])
    expect(screen.getByRole('heading', { name: '希望采用什么风格？' })).toBeInTheDocument()
    expect(screen.getByText('2 / 2')).toBeInTheDocument()
  })

  it('submits the complete batch with DSH answer semantics', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={questions} onSubmit={onSubmit} />)

    fireEvent.click(screen.getByRole('radio', { name: '微信公众号' }))
    fireEvent.click(screen.getByRole('button', { name: '下一题' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '克制' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      answers: [
        { id: 'platform', selected: ['微信公众号 (推荐)'] },
        { id: 'tone', selected: ['克制'] },
      ],
    }))
    expect(screen.getByText('已回答')).toBeInTheDocument()
  })

  it('defaults to a structured recommendation while keeping it editable', () => {
    render(<DshQuestionComposer questions={[{
      id: 'platform',
      question: '选择主平台',
      options: [
        { label: '公众号', description: '适合沉淀深度内容', recommended: true },
        { label: '小红书' },
      ],
    }]} onSubmit={vi.fn()} />)

    expect(screen.getByRole('radio', { name: '公众号' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('推荐')).toBeInTheDocument()
    expect(screen.getByText('适合沉淀深度内容')).toBeInTheDocument()
  })

  it('makes a custom single-select answer mutually exclusive', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={[questions[0]]} onSubmit={onSubmit} />)

    fireEvent.click(screen.getByRole('radio', { name: '微信公众号' }))
    fireEvent.change(screen.getByRole('textbox', { name: '平台补充说明' }), { target: { value: '视频号' } })
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      answers: [{ id: 'platform', selected: [], custom: '视频号' }],
    }))
  })

  it('keeps the draft editable after a failed submit', async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error('network down'))
    render(<DshQuestionComposer questions={[questions[0]]} onSubmit={onSubmit} />)

    fireEvent.click(screen.getByRole('radio', { name: '微信公众号' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(screen.getByText('network down')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: '提交回答' })).toBeEnabled()
  })

  it('keeps the submitted batch reviewable through read-only paging', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={questions} onSubmit={onSubmit} />)

    fireEvent.click(screen.getByRole('radio', { name: '微信公众号' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '克制' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(screen.getByText('已回答')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '展开问题卡片' }))
    expect(screen.getByText('克制')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '上一题' }))
    expect(screen.getByRole('heading', { name: '主要发布在哪个平台？' })).toBeInTheDocument()
    expect(screen.getByText(/^微信公众号/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '提交回答' })).not.toBeInTheDocument()
  })

  it('collapses an already submitted batch by default', () => {
    render(<DshQuestionComposer questions={[questions[0]]} answers={[{ id: 'platform', selected: ['微信公众号 (推荐)'] }]} submitted onSubmit={vi.fn()} />)

    expect(screen.getByRole('button', { name: '展开问题卡片' })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('微信公众号 (推荐)')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '展开问题卡片' }))
    expect(screen.getByText('微信公众号 (推荐)')).toBeInTheDocument()
  })

  it('collapses a batch after submitting its answers', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={[questions[0]]} onSubmit={onSubmit} />)

    fireEvent.click(screen.getByRole('radio', { name: '微信公众号' }))
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: '展开问题卡片' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '提交回答' })).not.toBeInTheDocument()
  })

  it('limits custom answers to the 20000 rune server limit before submitting', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={[{ id: 'details', question: '请补充说明' }]} onSubmit={onSubmit} />)

    const custom = '😀'.repeat(20001)
    fireEvent.change(screen.getByRole('textbox', { name: '问题补充说明' }), { target: { value: custom } })

    expect(screen.getByText('自定义答案最多 20000 个字符，超出部分已截断。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      answers: [{ id: 'details', selected: [], custom: '😀'.repeat(20000) }],
    }))
  })

  it('keeps long document answers editable and submits up to 20000 characters', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={[{ id: 'details', question: '请粘贴参考文档' }]} onSubmit={onSubmit} />)

    const custom = '参考内容\n'.repeat(3000)
    const textbox = screen.getByRole('textbox', { name: '问题补充说明' })
    fireEvent.change(textbox, { target: { value: custom } })

    expect(textbox).toHaveValue(custom)
    expect(screen.getByText(`${Array.from(custom).length} / 20000`)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      answers: [{ id: 'details', selected: [], custom }],
    }))
  })

  it('renders plan review questions with a readable review panel', () => {
    render(<DshQuestionComposer questions={[{
      id: 'plan',
      header: '执行计划',
      question: '确认执行这个计划吗？',
      detail: '先整理素材，再生成草稿。',
      intent: { kind: 'plan-review', approve: '执行计划' },
      options: [{ label: '执行计划' }, { label: '需要调整' }],
    }]} onSubmit={vi.fn()} />)

    expect(screen.getByText('计划审阅')).toBeInTheDocument()
    expect(screen.getAllByText('先整理素材，再生成草稿。')).toHaveLength(1)
    expect(screen.getByRole('radio', { name: '执行计划' })).toBeInTheDocument()
  })
})
