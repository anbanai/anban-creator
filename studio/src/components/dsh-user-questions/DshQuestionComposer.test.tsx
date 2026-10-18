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
    expect(screen.getByText('克制')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '上一题' }))
    expect(screen.getByRole('heading', { name: '主要发布在哪个平台？' })).toBeInTheDocument()
    expect(screen.getByText(/^微信公众号/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '提交回答' })).not.toBeInTheDocument()
  })

  it('limits custom answers to the server rune limit before submitting', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<DshQuestionComposer questions={[{ id: 'details', question: '请补充说明' }]} onSubmit={onSubmit} />)

    const custom = '😀'.repeat(2001)
    fireEvent.change(screen.getByRole('textbox', { name: '问题补充说明' }), { target: { value: custom } })

    expect(screen.getByText('自定义答案最多 2000 个字符，超出部分已截断。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '提交回答' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      answers: [{ id: 'details', selected: [], custom: '😀'.repeat(2000) }],
    }))
  })
})
