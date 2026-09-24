import { createEvent, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type {
  AgentPromptValue,
  AttachmentAdmissionPolicy,
  InputAttachment,
  PromptAttachment,
} from '@/types/input-attachment'
import {
  AttachmentRejectionReason,
  GENERAL_AGENT_ATTACHMENT_POLICY,
} from './attachment-admission'
import { AgentPromptDropProvider } from './AgentPromptDropProvider'
import {
  AgentPromptInput,
  type AgentPromptInputProps,
} from './AgentPromptInput'
import {
  usePromptAttachments,
  type PromptAttachmentsController,
} from './usePromptAttachments'

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
    move: vi.fn(),
    reset: vi.fn(),
    clear: vi.fn(),
    uploading: false,
    hasFailures: false,
    toInputAttachments: vi.fn(() => []),
    localFiles: vi.fn(() => []),
    previewSource: vi.fn(),
    sourceAttachment: vi.fn(),
    ...overrides,
  }
}

function ControlledPromptHarness() {
  const [value, setValue] = useState<AgentPromptValue>({ prompt: '', attachments: [] })
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'local' },
    policy,
    attachments: value.attachments,
    onAttachmentsChange: (attachments) => {
      setValue((current) => ({ ...current, attachments }))
    },
    createId: (() => {
      let id = 0
      return () => `controlled-${++id}`
    })(),
    createObjectURL: (file) => `blob:${file.name}`,
    revokeObjectURL: vi.fn(),
  })
  const replacement: InputAttachment = {
    type: 'text',
    text: 'task B',
    file_name: 'task-b.txt',
  }

  return (
    <>
      <button type="button" onClick={() => attachmentController.reset([replacement])}>
        Reset task
      </button>
      <output data-testid="controlled-attachment-names">
        {value.attachments.map((item) => item.fileName).join(',')}
      </output>
      <AgentPromptInput
        value={value}
        onChange={setValue}
        onSubmit={vi.fn()}
        attachmentController={attachmentController}
        attachmentPolicy={policy}
      />
    </>
  )
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
  it('supports a prompt-only composer without a duplicate file picker or file admission', () => {
    const { props } = renderPrompt({ attachmentsEnabled: false, ariaLabel: '复刻要求' })
    const prompt = screen.getByLabelText('复刻要求')
    expect(screen.queryByRole('button', { name: '添加附件' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('选择附件文件')).not.toBeInTheDocument()
    fireEvent.change(prompt, { target: { value: '保留镜头节奏' } })
    expect(props.onChange).toHaveBeenCalledWith({ prompt: '保留镜头节奏', attachments: [] })
    fireEvent.paste(prompt, { clipboardData: { files: [image()] } })
    fireEvent.drop(prompt, { dataTransfer: dragData([image()]) })
    expect(props.attachmentController.addFiles).not.toHaveBeenCalled()
  })

  it('offers every document and text extension accepted by the shared policy', () => {
    renderPrompt({ attachmentPolicy: GENERAL_AGENT_ATTACHMENT_POLICY })

    const accepted = screen.getByLabelText('选择附件文件').getAttribute('accept')?.split(',')
    expect(accepted).toEqual(expect.arrayContaining(['.json', '.csv', '.md', '.markdown', '.txt']))
  })

  it('renders the stable section, surface, content order, and toolbar slots', () => {
    const current = attachment()
    renderPrompt({
      value: { prompt: 'Draft', attachments: [current] },
      attachmentController: controller({
        attachments: [attachment({ id: 'controller-only', fileName: 'controller-only.png' })],
      }),
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
    const row = within(root).getByRole('button', { name: '预览 reference.png' }).closest('[data-slot="agent-prompt-attachment"]')
    const textarea = screen.getByRole('textbox', { name: 'Agent request' })
    expect(row?.compareDocumentPosition(textarea)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)

    const footer = root.querySelector('[data-slot="input-group-addon"][data-align="block-end"]')
    expect(footer).toContainElement(screen.getByRole('button', { name: 'Leading tool' }))
    expect(footer).toContainElement(screen.getByText('Ready'))
    expect(footer).toContainElement(screen.getByRole('button', { name: 'Trailing tool' }))
    expect(screen.getByPlaceholderText('Ask the agent')).toBe(textarea)
    expect(screen.queryByText('controller-only.png')).not.toBeInTheDocument()
  })

  it('reports prompt changes with the value-owned attachments', () => {
    const current = attachment()
    const onChange = vi.fn()
    renderPrompt({
      value: { prompt: '', attachments: [current] },
      attachmentController: controller({
        attachments: [attachment({ id: 'controller-only', fileName: 'controller-only.png' })],
      }),
      onChange,
    })

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'New prompt' } })

    expect(onChange).toHaveBeenCalledWith({ prompt: 'New prompt', attachments: [current] })
  })

  it('opens local and inherited attachment previews without stealing row actions', async () => {
    const localFile = image('local.png')
    const local = attachment({
      id: 'local',
      file: localFile,
      fileName: localFile.name,
      size: localFile.size,
    })
    const inherited = attachment({
      id: 'inherited',
      type: 'text',
      fileName: 'brief.txt',
      contentType: 'text/plain',
      size: 5,
    })
    const remove = vi.fn()
    const attachmentController = controller({
      remove,
      previewSource: vi.fn((id) => id === local.id ? 'blob:local.png' : undefined),
      sourceAttachment: vi.fn((id: string): InputAttachment | undefined => id === inherited.id
        ? { type: 'text', text: 'brief', file_name: 'brief.txt', size: 5 }
        : undefined),
    })
    renderPrompt({
      value: { prompt: '', attachments: [local, inherited] },
      attachmentController,
    })

    expect(screen.getByRole('img', { name: 'local.png 附件缩略图' })).toHaveAttribute('src', 'blob:local.png')
    expect(document.querySelector('[data-slot="agent-prompt-attachments"]')).toHaveClass('flex-row', 'flex-wrap')

    fireEvent.click(screen.getByRole('button', { name: '预览 local.png' }))
    expect(await screen.findByRole('img', { name: 'local.png' })).toHaveAttribute('src', 'blob:local.png')
    fireEvent.click(screen.getByRole('button', { name: '关闭附件预览' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '预览 brief.txt' }))
    expect(await screen.findByTestId('attachment-text-preview')).toHaveTextContent('brief')
    expect(remove).not.toHaveBeenCalled()
    expect(attachmentController.previewSource).toHaveBeenCalledWith(local.id)
    expect(attachmentController.sourceAttachment).toHaveBeenCalledWith(inherited.id)
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
      value: { prompt: '', attachments: [attachment()] },
      attachmentController: controller({ addFiles }),
      acceptedTypesLabel: '图片或文档',
    })
    fireEvent.focus(screen.getByRole('textbox'))
    const dataTransfer = dragData([file])

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toHaveTextContent('还可添加 2 个')
    fireEvent.drop(document.body, { dataTransfer })

    expect(addFiles).toHaveBeenCalledOnce()
    expect(addFiles).toHaveBeenCalledWith([file])
  })

  it('routes an unsupported global drop through controller admission and announces rejection', () => {
    const file = new File(['binary'], 'script.exe', { type: 'application/x-msdownload' })
    const rejection = { file, reason: AttachmentRejectionReason.UnsupportedType }
    const addFiles = vi.fn(() => ({ accepted: [], rejected: [rejection] }))
    const onAttachmentRejected = vi.fn()
    renderPrompt({
      attachmentController: controller({ addFiles }),
      onAttachmentRejected,
      acceptedTypesLabel: '图片或文档',
    })
    fireEvent.focus(screen.getByRole('textbox'))
    const dataTransfer = dragData([file])

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
    fireEvent.drop(document.body, { dataTransfer })

    expect(addFiles).toHaveBeenCalledOnce()
    expect(addFiles).toHaveBeenCalledWith([file])
    expect(onAttachmentRejected).toHaveBeenCalledWith([rejection])
    expect(screen.getByRole('alert')).toHaveTextContent('拒绝 1 个附件')
    expect(screen.getByRole('alert')).toHaveTextContent('不支持的类型')
    expect(document.querySelector('[data-slot="agent-prompt-attachment"]')).not.toBeInTheDocument()
  })

  it('submits on Enter, preserves Shift+Enter, and ignores IME events', () => {
    const onSubmit = vi.fn()
    const current = attachment()
    renderPrompt({
      value: { prompt: 'Submit me', attachments: [current] },
      attachmentController: controller({
        attachments: [attachment({ id: 'controller-only', fileName: 'controller-only.png' })],
      }),
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

  it.each(['keyboard', 'button'] as const)('contains %s submit errors and allows a successful retry', async (source) => {
    const error = new Error('submit failed')
    const onSubmit = vi.fn()
      .mockRejectedValueOnce(error)
      .mockResolvedValueOnce(undefined)
    const onSubmitError = vi.fn()
    renderPrompt({ onSubmit, onSubmitError, submitLabel: 'Run agent' })
    const textarea = screen.getByRole('textbox')
    const button = screen.getByRole('button', { name: 'Run agent' })

    if (source === 'keyboard') fireEvent.keyDown(textarea, { key: 'Enter' })
    else fireEvent.click(button)

    await waitFor(() => expect(onSubmitError).toHaveBeenCalledWith(error))
    expect(onSubmitError).toHaveBeenCalledOnce()
    expect(screen.getByText('提交失败，请重试')).toBeInTheDocument()
    await waitFor(() => expect(button).toBeEnabled())

    fireEvent.click(button)
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2))
    expect(onSubmitError).toHaveBeenCalledOnce()
    await waitFor(() => expect(screen.queryByText('提交失败，请重试')).not.toBeInTheDocument())
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

  it('leaves submission to the surrounding form in external mode', () => {
    const onSubmit = vi.fn()
    renderPrompt({ onSubmit, submitMode: 'external', submitLabel: 'Create task' })

    expect(screen.queryByRole('button', { name: 'Create task' })).not.toBeInTheDocument()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    expect(onSubmit).not.toHaveBeenCalled()
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
      value: { prompt: '', attachments: [failed] },
      attachmentController: controller({
        hasFailures: true,
        retry,
        remove,
        updateInstruction,
      }),
    })

    const row = screen.getByRole('button', { name: '预览 reference.png' }).closest('[data-slot="agent-prompt-attachment"]') as HTMLElement
    expect(row).toHaveClass('size-20')
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

  it('keeps long attachment metadata bounded and announces transient and failed states', () => {
    const longName = `${'very-long-file-name-'.repeat(12)}.png`
    const longError = 'networkfailure'.repeat(40)
    const queued = attachment({ id: 'queued', fileName: longName, status: 'queued', progress: 0 })
    const uploading = attachment({ id: 'uploading', fileName: 'uploading.png', status: 'uploading', progress: 42 })
    const failed = attachment({ id: 'failed', fileName: 'failed.png', status: 'failed', progress: 0, error: longError })
    renderPrompt({
      value: { prompt: '', attachments: [queued, uploading, failed] },
      attachmentController: controller({ hasFailures: true }),
    })

    const queuedRow = screen.getByRole('button', { name: `预览 ${longName}` }).closest('[data-slot="agent-prompt-attachment"]') as HTMLElement
    expect(queuedRow).toHaveClass('size-20', 'shrink-0', 'overflow-hidden')
    expect(queuedRow.querySelector('[data-slot="agent-prompt-attachment-name"]')).toHaveClass('min-w-0', 'truncate')
    expect(queuedRow.querySelector('[data-slot="agent-prompt-attachment-meta"]')).toHaveClass('w-full', 'min-w-0')
    expect(within(queuedRow).getByText('1.5 KB')).toBeInTheDocument()
    expect(within(queuedRow).getByText('等待中').closest('[data-slot="agent-prompt-attachment-status"]')).toHaveClass('shrink-0')
    expect(within(queuedRow).getByRole('status')).toHaveAttribute('aria-live', 'polite')

    expect(screen.getByRole('status', { name: 'uploading.png 状态' })).toHaveTextContent('上传中')
    const failureAlert = screen.getByRole('alert', { name: 'failed.png 状态' })
    expect(failureAlert).toHaveTextContent('失败')
    expect(failureAlert).toHaveTextContent(longError)
    expect(failureAlert.querySelector('[data-slot="agent-prompt-attachment-error"]')).toHaveClass('min-w-0', 'truncate')
    expect(within(queuedRow).getByRole('button', { name: `删除 ${longName}` })).toHaveAttribute('data-size', 'icon-xs')
  })

  it('announces the attachment lifecycle through uploaded completion, then uses an alert for failure', () => {
    const queued = attachment({ id: 'transition', fileName: 'transition.png', status: 'queued', progress: 0 })
    const { props, rerender } = renderPrompt({
      value: { prompt: '', attachments: [queued] },
    })

    const renderState = (next: PromptAttachment) => {
      rerender(
        <AgentPromptInput
          {...props}
          value={{ prompt: '', attachments: [next] }}
        />,
      )
    }
    const expectPoliteStatus = (label: string) => {
      const row = screen.getByRole('button', { name: '预览 transition.png' }).closest('[data-slot="agent-prompt-attachment"]') as HTMLElement
      const region = within(row).getByRole('status', { name: 'transition.png 状态' })
      expect(region).toHaveAttribute('aria-live', 'polite')
      expect(region).toHaveAttribute('aria-atomic', 'true')
      expect(region).toHaveTextContent(label)
      expect(within(region).getAllByText(label)).toHaveLength(1)
      expect(within(row).getAllByRole('status')).toHaveLength(1)
    }

    expectPoliteStatus('等待中')
    renderState({ ...queued, status: 'uploading', progress: 45 })
    expectPoliteStatus('上传中')
    renderState({ ...queued, status: 'uploaded', progress: 100 })
    expectPoliteStatus('已上传')

    renderState({ ...queued, status: 'failed', error: 'Upload failed' })
    const row = screen.getByRole('button', { name: '预览 transition.png' }).closest('[data-slot="agent-prompt-attachment"]') as HTMLElement
    expect(within(row).queryByRole('status')).not.toBeInTheDocument()
    expect(within(row).getAllByRole('alert')).toHaveLength(1)
    expect(within(row).getByRole('alert', { name: 'transition.png 状态' })).toHaveTextContent('失败')
  })

  it('updates parent-owned attachment values through controller mutations and reset', () => {
    render(<ControlledPromptHarness />, { wrapper: Provider })
    const file = image('controlled.png')

    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })
    expect(screen.getByTestId('controlled-attachment-names')).toHaveTextContent('controlled.png')
    const composer = document.querySelector('[data-slot="agent-prompt-input"]') as HTMLElement
    expect(within(composer).getByRole('button', { name: '预览 controlled.png' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '删除 controlled.png' }))
    expect(screen.getByTestId('controlled-attachment-names')).toBeEmptyDOMElement()
    expect(screen.queryByText('controlled.png')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Reset task' }))
    expect(screen.getByTestId('controlled-attachment-names')).toHaveTextContent('task-b.txt')
    expect(within(composer).getByRole('button', { name: '预览 task-b.txt' })).toBeInTheDocument()
  })
})
