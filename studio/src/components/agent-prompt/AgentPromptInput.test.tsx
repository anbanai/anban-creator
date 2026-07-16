import { createEvent, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type {
  AttachmentAdmissionPolicy,
  PromptAttachment,
} from '@/types/input-attachment'
import { AttachmentRejectionReason } from './attachment-admission'
import { AgentPromptDropProvider } from './AgentPromptDropProvider'
import {
  AgentPromptInput,
  type AgentPromptInputProps,
} from './AgentPromptInput'
import type { PromptAttachmentsController } from './usePromptAttachments'

const policy: AttachmentAdmissionPolicy = {
  allowedTypes: ['image', 'document'],
  maxCount: 3,
}

function image(name = 'reference.png') {
  return new File(['image'], name, { type: 'image/png' })
}

function dragData(files: File[]) {
  return {
    types: ['Files'],
    files,
    items: files.map((file) => ({ kind: 'file', type: file.type })),
    dropEffect: 'none',
  } as unknown as DataTransfer
}

function attachment(overrides: Partial<PromptAttachment> = {}): PromptAttachment {
  return {
    id: 'attachment-1',
    type: 'image',
    fileName: 'reference.png',
    contentType: 'image/png',
    size: 1_500,
    status: 'uploaded',
    progress: 100,
    ...overrides,
  }
}

function controller(
  overrides: Partial<PromptAttachmentsController> = {},
): PromptAttachmentsController {
  return {
    attachments: [],
    addFiles: vi.fn(() => ({ accepted: [], rejected: [] })),
    retry: vi.fn(),
    remove: vi.fn(),
    updateInstruction: vi.fn(),
    clear: vi.fn(),
    uploading: false,
    hasFailures: false,
    toInputAttachments: vi.fn(() => []),
    localFiles: vi.fn(() => []),
    previewSource: vi.fn(),
    ...overrides,
  }
}

function Provider({ children }: { children: ReactNode }) {
  return <AgentPromptDropProvider>{children}</AgentPromptDropProvider>
}

function renderPrompt(overrides: Partial<AgentPromptInputProps> = {}) {
  const props: AgentPromptInputProps = {
    value: { prompt: 'Draft', attachments: [] },
    onChange: vi.fn(),
    onSubmit: vi.fn(),
    attachmentController: controller(),
    attachmentPolicy: policy,
    ...overrides,
  }
  return { props, ...render(<AgentPromptInput {...props} />, { wrapper: Provider }) }
}

describe('AgentPromptInput', () => {
  it('renders the stable section, surface, content order, and toolbar slots', () => {
    const current = attachment()
    renderPrompt({
      value: { prompt: 'Draft', attachments: [] },
      attachmentController: controller({ attachments: [current] }),
      contextBar: <span>Project context</span>,
      leadingTools: <button type="button">Leading tool</button>,
      trailingTools: <button type="button">Trailing tool</button>,
      status: <span>Ready</span>,
      placeholder: 'Ask the agent',
      ariaLabel: 'Agent request',
    })

    const root = document.querySelector('[data-slot="agent-prompt-input"]') as HTMLElement
    expect(root.tagName).toBe('SECTION')
    expect(root).toHaveAttribute('data-slot', 'agent-prompt-input')
    expect(root).not.toHaveAttribute('data-size')
    expect(root.closest('form')).toBeNull()
    expect(screen.getByText('Project context')).toBeInTheDocument()

    const surface = root.querySelector('[data-slot="agent-prompt-surface"]')
    expect(surface).toHaveClass('min-h-40', 'h-auto', 'flex-col', 'overflow-hidden')
    expect(surface?.className).toContain('max-h-[min(42rem,calc(100dvh-8rem))]')
    const row = within(root).getByText('reference.png').closest('[data-slot="agent-prompt-attachment"]')
    const textarea = screen.getByRole('textbox', { name: 'Agent request' })
    expect(row?.compareDocumentPosition(textarea)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)

    const footer = root.querySelector('[data-slot="input-group-addon"][data-align="block-end"]')
    expect(footer).toContainElement(screen.getByRole('button', { name: 'Leading tool' }))
    expect(footer).toContainElement(screen.getByText('Ready'))
    expect(footer).toContainElement(screen.getByRole('button', { name: 'Trailing tool' }))
    expect(screen.getByPlaceholderText('Ask the agent')).toBe(textarea)
  })

  it('reports prompt changes with the controller-owned attachments', () => {
    const current = attachment()
    const onChange = vi.fn()
    renderPrompt({
      value: { prompt: '', attachments: [] },
      attachmentController: controller({ attachments: [current] }),
      onChange,
    })

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'New prompt' } })

    expect(onChange).toHaveBeenCalledWith({ prompt: 'New prompt', attachments: [current] })
  })

  it('resets the picker, handles file paste, and leaves text-only paste untouched', () => {
    const file = image()
    const addFiles = vi.fn(() => ({ accepted: [{ file, type: 'image' as const }], rejected: [] }))
    renderPrompt({ attachmentController: controller({ addFiles }) })
    const picker = screen.getByLabelText('选择附件文件') as HTMLInputElement

    fireEvent.change(picker, { target: { files: [file] } })
    expect(addFiles).toHaveBeenLastCalledWith([file])
    expect(picker.value).toBe('')

    const textarea = screen.getByRole('textbox')
    const filePaste = createEvent.paste(textarea)
    Object.defineProperty(filePaste, 'clipboardData', { value: { files: [file] } })
    fireEvent(textarea, filePaste)
    expect(filePaste.defaultPrevented).toBe(true)
    expect(addFiles).toHaveBeenCalledTimes(2)

    const textPaste = createEvent.paste(textarea)
    Object.defineProperty(textPaste, 'clipboardData', { value: { files: [] } })
    fireEvent(textarea, textPaste)
    expect(textPaste.defaultPrevented).toBe(false)
    expect(addFiles).toHaveBeenCalledTimes(2)
  })

  it('handles a local drop once without leaking it to the global provider', () => {
    const file = image()
    const addFiles = vi.fn(() => ({ accepted: [{ file, type: 'image' as const }], rejected: [] }))
    renderPrompt({ attachmentController: controller({ addFiles }) })
    const root = document.querySelector('[data-slot="agent-prompt-input"]') as HTMLElement
    const dataTransfer = dragData([file])

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    expect(fireEvent.drop(root, { dataTransfer })).toBe(false)

    expect(addFiles).toHaveBeenCalledOnce()
    expect(addFiles).toHaveBeenCalledWith([file])
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('registers a focused, capacity-aware global drop target', () => {
    const file = image()
    const addFiles = vi.fn(() => ({ accepted: [{ file, type: 'image' as const }], rejected: [] }))
    renderPrompt({
      attachmentController: controller({
        attachments: [attachment()],
        addFiles,
      }),
      acceptedTypesLabel: '图片或文档',
    })
    fireEvent.focus(screen.getByRole('textbox'))
    const dataTransfer = dragData([file])

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.getByRole('status')).toHaveTextContent('还可添加 2 个')
    fireEvent.drop(document.body, { dataTransfer })

    expect(addFiles).toHaveBeenCalledOnce()
    expect(addFiles).toHaveBeenCalledWith([file])
  })

  it('submits on Enter, preserves Shift+Enter, and ignores IME events', () => {
    const onSubmit = vi.fn()
    const current = attachment()
    renderPrompt({
      value: { prompt: 'Submit me', attachments: [] },
      attachmentController: controller({ attachments: [current] }),
      onSubmit,
    })
    const textarea = screen.getByRole('textbox')

    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })
    fireEvent.compositionStart(textarea)
    fireEvent.keyDown(textarea, { key: 'Enter' })
    fireEvent.compositionEnd(textarea)
    fireEvent.keyDown(textarea, { key: 'Enter', isComposing: true })
    fireEvent.keyDown(textarea, { key: 'Enter', keyCode: 229 })
    expect(onSubmit).not.toHaveBeenCalled()

    expect(fireEvent.keyDown(textarea, { key: 'Enter' })).toBe(false)
    expect(onSubmit).toHaveBeenCalledWith({ prompt: 'Submit me', attachments: [current] })
  })

  it('guards an asynchronous submit against button and keyboard duplicates', async () => {
    let resolveSubmit!: () => void
    const pending = new Promise<void>((resolve) => { resolveSubmit = resolve })
    const onSubmit = vi.fn(() => pending)
    renderPrompt({ onSubmit, submitLabel: 'Run agent' })
    const textarea = screen.getByRole('textbox')
    const button = screen.getByRole('button', { name: 'Run agent' })

    fireEvent.click(button)
    fireEvent.click(button)
    fireEvent.keyDown(textarea, { key: 'Enter' })

    expect(onSubmit).toHaveBeenCalledOnce()
    expect(button).toBeDisabled()
    resolveSubmit()
  })

  it.each([
    ['disabled', { disabled: true }],
    ['submitting', { submitting: true }],
    ['submitDisabled', { submitDisabled: true }],
    ['uploading', { attachmentController: controller({ uploading: true }) }],
    ['failed', { attachmentController: controller({ hasFailures: true }) }],
  ] satisfies Array<[string, Partial<AgentPromptInputProps>]>)('blocks submit while %s', (_name, blockedProps) => {
    const onSubmit = vi.fn()
    renderPrompt({ ...blockedProps, onSubmit })
    const button = screen.getByRole('button', { name: 'Submit' })

    expect(button).toBeDisabled()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    fireEvent.click(button)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('announces rejected files with counts and reasons', () => {
    const file = new File(['x'], 'script.exe', { type: 'application/x-msdownload' })
    const onAttachmentRejected = vi.fn()
    const addFiles = vi.fn(() => ({
      accepted: [],
      rejected: [{ file, reason: AttachmentRejectionReason.UnsupportedType }],
    }))
    renderPrompt({
      attachmentController: controller({ addFiles }),
      onAttachmentRejected,
    })

    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })

    expect(onAttachmentRejected).toHaveBeenCalledWith([
      { file, reason: AttachmentRejectionReason.UnsupportedType },
    ])
    expect(screen.getByRole('alert')).toHaveTextContent('拒绝 1 个附件')
    expect(screen.getByRole('alert')).toHaveTextContent('不支持的类型')
  })

  it('edits instructions and exposes retry and remove attachment actions', async () => {
    const failed = attachment({ status: 'failed', progress: 0, error: 'Network error' })
    const retry = vi.fn()
    const remove = vi.fn()
    const updateInstruction = vi.fn()
    renderPrompt({
      attachmentController: controller({
        attachments: [failed],
        hasFailures: true,
        retry,
        remove,
        updateInstruction,
      }),
    })

    const row = screen.getByText('reference.png').closest('[data-slot="agent-prompt-attachment"]') as HTMLElement
    expect(row).toHaveClass('min-h-10')
    expect(within(row).getByText('1.5 KB')).toBeInTheDocument()
    expect(within(row).getByText('失败')).toBeInTheDocument()
    expect(within(row).getByText('Network error')).toBeInTheDocument()

    fireEvent.click(within(row).getByRole('button', { name: '重试 reference.png' }))
    fireEvent.click(within(row).getByRole('button', { name: '删除 reference.png' }))
    expect(retry).toHaveBeenCalledWith('attachment-1')
    expect(remove).toHaveBeenCalledWith('attachment-1')

    fireEvent.click(within(row).getByRole('button', { name: '编辑 reference.png 的附件说明' }))
    const input = await screen.findByRole('textbox', { name: '附件说明' })
    expect(input).toHaveAttribute('maxlength', '1000')
    fireEvent.change(input, { target: { value: 'Focus on the subject' } })
    expect(updateInstruction).toHaveBeenCalledWith('attachment-1', 'Focus on the subject')
  })
})
