import { useState, type DragEventHandler } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  AgentPromptDropProvider,
  useAgentPromptDropTarget,
} from './AgentPromptDropProvider'

interface TargetHarnessProps {
  name: string
  onFiles: (files: File[]) => void
  enabled?: boolean
  remainingCapacity?: number
  acceptedTypesLabel?: string
  acceptsFile?: (file: File) => boolean
  acceptsItemType?: (mime: string) => boolean
  onDrop?: DragEventHandler<HTMLDivElement>
}

function TargetHarness({
  name,
  onFiles,
  enabled = true,
  remainingCapacity = 3,
  acceptedTypesLabel = '图片或文档',
  acceptsFile = () => true,
  acceptsItemType,
  onDrop,
}: TargetHarnessProps) {
  const dropTarget = useAgentPromptDropTarget({
    enabled,
    remainingCapacity,
    acceptedTypesLabel,
    acceptsFile,
    acceptsItemType,
    onFiles,
  })

  return (
    <div
      ref={dropTarget.ref}
      data-testid={`target-${name}`}
      onFocusCapture={dropTarget.onFocusCapture}
      onDrop={onDrop}
      tabIndex={0}
    >
      {name}
    </div>
  )
}

function dragData({
  files = [],
  types = ['Files'],
  itemTypes = files.map((file) => file.type),
}: {
  files?: File[]
  types?: string[]
  itemTypes?: string[]
} = {}) {
  return {
    types,
    files,
    items: itemTypes.map((type) => ({ kind: 'file', type })),
    dropEffect: 'none',
  } as unknown as DataTransfer
}

function image(name = 'reference.png') {
  return new File(['image'], name, { type: 'image/png' })
}

function documentDrop(dataTransfer: DataTransfer) {
  return fireEvent.drop(document.body, { dataTransfer })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AgentPromptDropProvider', () => {
  it('delivers to the last-focused eligible target', () => {
    const first = vi.fn()
    const second = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="first" onFiles={first} />
        <TargetHarness name="second" onFiles={second} />
      </AgentPromptDropProvider>,
    )
    fireEvent.focus(screen.getByTestId('target-first'))
    fireEvent.focus(screen.getByTestId('target-second'))
    const file = image()

    fireEvent.dragEnter(document.body, { dataTransfer: dragData({ files: [file] }) })
    documentDrop(dragData({ files: [file] }))

    expect(second).toHaveBeenCalledOnce()
    expect(second).toHaveBeenCalledWith([file])
    expect(first).not.toHaveBeenCalled()
  })

  it('scopes delivery to the topmost open Dialog', async () => {
    const page = vi.fn()
    const dialog = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="page" onFiles={page} />
        <Dialog open>
          <DialogContent>
            <DialogTitle>Attach a file</DialogTitle>
            <TargetHarness name="dialog" onFiles={dialog} />
          </DialogContent>
        </Dialog>
      </AgentPromptDropProvider>,
    )
    fireEvent.focus(screen.getByTestId('target-page'))
    const dialogTarget = await screen.findByTestId('target-dialog')
    fireEvent.focus(dialogTarget)
    const file = image()

    fireEvent.dragEnter(dialogTarget, { dataTransfer: dragData({ files: [file] }) })
    documentDrop(dragData({ files: [file] }))

    expect(dialog).toHaveBeenCalledWith([file])
    expect(page).not.toHaveBeenCalled()
  })

  it('does not leak to the page when the top Dialog has no target', async () => {
    const page = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="page" onFiles={page} />
        <Dialog open>
          <DialogContent>
            <DialogTitle>Blocking dialog</DialogTitle>
            <p>No prompt here</p>
          </DialogContent>
        </Dialog>
      </AgentPromptDropProvider>,
    )
    await screen.findByText('No prompt here')
    fireEvent.focus(screen.getByTestId('target-page'))
    const dataTransfer = dragData({ files: [image()] })

    fireEvent.dragEnter(document.body, { dataTransfer })

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
    expect(documentDrop(dataTransfer)).toBe(false)
    expect(page).not.toHaveBeenCalled()
  })

  it('treats an open AlertDialog above the target as a blocker', async () => {
    const dialog = vi.fn()
    render(
      <AgentPromptDropProvider>
        <Dialog open>
          <DialogContent>
            <DialogTitle>Prompt dialog</DialogTitle>
            <TargetHarness name="dialog" onFiles={dialog} />
          </DialogContent>
        </Dialog>
        <AlertDialog open>
          <AlertDialogContent>
            <AlertDialogTitle>Confirm destructive action</AlertDialogTitle>
          </AlertDialogContent>
        </AlertDialog>
      </AgentPromptDropProvider>,
    )
    await screen.findByText('Confirm destructive action')
    const dataTransfer = dragData({ files: [image()] })

    fireEvent.dragEnter(document.body, { dataTransfer })
    documentDrop(dataTransfer)

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
    expect(dialog).not.toHaveBeenCalled()
  })

  it('uses the sole eligible target as a deterministic fallback', () => {
    const onFiles = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={onFiles} />
      </AgentPromptDropProvider>,
    )
    const file = image()
    const dataTransfer = dragData({ files: [file] })

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    documentDrop(dataTransfer)

    expect(onFiles).toHaveBeenCalledWith([file])
  })

  it('accepts mixed transfer types containing Files and delivers once', () => {
    const onFiles = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={onFiles} />
      </AgentPromptDropProvider>,
    )
    const file = image()
    const dataTransfer = dragData({
      files: [file],
      types: ['text/uri-list', 'Files'],
    })

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    expect(fireEvent.dragOver(document.body, { dataTransfer })).toBe(false)
    expect(documentDrop(dataTransfer)).toBe(false)

    expect(onFiles).toHaveBeenCalledOnce()
    expect(onFiles).toHaveBeenCalledWith([file])
  })

  it('keeps an empty live status mounted, then updates and clears it', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={vi.fn()} />
      </AgentPromptDropProvider>,
    )
    const status = screen.getByRole('status')
    expect(status).toBeEmptyDOMElement()
    const dataTransfer = dragData({ files: [image()] })

    fireEvent.dragEnter(document.body, { dataTransfer })
    expect(status).toHaveTextContent('释放以添加 1 个文件')

    fireEvent.dragLeave(document.body, { dataTransfer })
    expect(status).toBeEmptyDOMElement()
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('shows the dragged file count, accepted types, and selected target capacity', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness
          name="only"
          onFiles={vi.fn()}
          acceptedTypesLabel="图片或文档"
          remainingCapacity={3}
        />
      </AgentPromptDropProvider>,
    )
    const files = [image('first.png'), image('second.png')]
    const status = '释放以添加 2 个文件 · 支持图片或文档 · 还可添加 3 个'

    fireEvent.dragEnter(document.body, { dataTransfer: dragData({ files }) })

    expect(screen.getByTestId('agent-prompt-drop-overlay')).toHaveTextContent(status)
    expect(screen.getByRole('status')).toHaveTextContent(status)
  })

  it('refreshes the active overlay and live status when capacity changes', () => {
    const onFiles = vi.fn()
    const files = [image('first.png'), image('second.png')]
    const { rerender } = render(
      <AgentPromptDropProvider>
        <TargetHarness
          name="only"
          onFiles={onFiles}
          acceptedTypesLabel="图像"
          remainingCapacity={3}
        />
      </AgentPromptDropProvider>,
    )
    fireEvent.dragEnter(document.body, { dataTransfer: dragData({ files }) })

    rerender(
      <AgentPromptDropProvider>
        <TargetHarness
          name="only"
          onFiles={onFiles}
          acceptedTypesLabel="图像"
          remainingCapacity={1}
        />
      </AgentPromptDropProvider>,
    )
    const status = '释放以添加 2 个文件 · 支持图像 · 还可添加 1 个'

    expect(screen.getByTestId('agent-prompt-drop-overlay')).toHaveTextContent(status)
    expect(screen.getByRole('status')).toHaveTextContent(status)
  })

  it('does not guess between multiple eligible targets without focus', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="first" onFiles={vi.fn()} />
        <TargetHarness name="second" onFiles={vi.fn()} />
      </AgentPromptDropProvider>,
    )

    fireEvent.dragEnter(document.body, { dataTransfer: dragData() })

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('keeps the overlay active until nested drag depth reaches zero', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={vi.fn()} />
      </AgentPromptDropProvider>,
    )
    const target = screen.getByTestId('target-only')
    const dataTransfer = dragData()

    fireEvent.dragEnter(target, { dataTransfer })
    fireEvent.dragEnter(target.firstChild as Node, { dataTransfer })
    fireEvent.dragLeave(target.firstChild as Node, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()

    fireEvent.dragLeave(target, { dataTransfer })
    fireEvent.dragLeave(target, { dataTransfer })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('ignores text and wholly unsupported file drags but treats empty MIME as potential', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness
          name="images"
          onFiles={vi.fn()}
          acceptsFile={(file) => file.type.startsWith('image/')}
          acceptsItemType={(mime) => mime.startsWith('image/')}
        />
      </AgentPromptDropProvider>,
    )

    fireEvent.dragEnter(document.body, {
      dataTransfer: dragData({ types: ['text/plain'], itemTypes: [] }),
    })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()

    fireEvent.dragEnter(document.body, {
      dataTransfer: dragData({ itemTypes: ['video/mp4'] }),
    })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()

    fireEvent.dragEnter(document.body, {
      dataTransfer: dragData({ itemTypes: [''] }),
    })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()

    fireEvent.dragOver(document.body, {
      dataTransfer: dragData({ itemTypes: ['video/mp4'] }),
    })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('clears the overlay on Escape', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={vi.fn()} />
      </AgentPromptDropProvider>,
    )
    fireEvent.dragEnter(document.body, { dataTransfer: dragData() })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('clears the overlay when the selected target unmounts', () => {
    function Example() {
      const [mounted, setMounted] = useState(true)
      return (
        <AgentPromptDropProvider>
          <button onClick={() => setMounted(false)}>Unmount</button>
          {mounted ? <TargetHarness name="only" onFiles={vi.fn()} /> : null}
        </AgentPromptDropProvider>
      )
    }
    render(<Example />)
    fireEvent.dragEnter(document.body, { dataTransfer: dragData() })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Unmount' }))

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('clears the overlay when rerendered at zero capacity', () => {
    const onFiles = vi.fn()
    const { rerender } = render(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={onFiles} remainingCapacity={1} />
      </AgentPromptDropProvider>,
    )
    fireEvent.dragEnter(document.body, { dataTransfer: dragData() })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()

    rerender(
      <AgentPromptDropProvider>
        <TargetHarness name="only" onFiles={onFiles} remainingCapacity={0} />
      </AgentPromptDropProvider>,
    )

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('delivers the whole File array exactly once when at least one file is accepted', () => {
    const onFiles = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness
          name="only"
          onFiles={onFiles}
          acceptsFile={(file) => file.type.startsWith('image/')}
          acceptsItemType={(mime) => mime.startsWith('image/')}
        />
      </AgentPromptDropProvider>,
    )
    const accepted = image()
    const unsupported = new File(['text'], 'notes.txt', { type: 'text/plain' })
    const dataTransfer = dragData({ files: [accepted, unsupported] })

    fireEvent.dragEnter(document.body, { dataTransfer })
    documentDrop(dataTransfer)

    expect(onFiles).toHaveBeenCalledOnce()
    expect(onFiles).toHaveBeenCalledWith([accepted, unsupported])
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('does not deliver when no actual dropped file is accepted', () => {
    const onFiles = vi.fn()
    render(
      <AgentPromptDropProvider>
        <TargetHarness
          name="images"
          onFiles={onFiles}
          acceptsFile={(file) => file.type.startsWith('image/')}
        />
      </AgentPromptDropProvider>,
    )
    const text = new File(['text'], 'notes.txt', { type: 'text/plain' })

    expect(documentDrop(dragData({ files: [text] }))).toBe(false)
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('defers to a local drop handler that already prevented the event', () => {
    const onFiles = vi.fn()
    const localDrop = vi.fn<DragEventHandler<HTMLDivElement>>((event) => {
      event.preventDefault()
      event.stopPropagation()
    })
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="local" onFiles={onFiles} onDrop={localDrop} />
      </AgentPromptDropProvider>,
    )
    const target = screen.getByTestId('target-local')
    const file = image()
    const dataTransfer = dragData({ files: [file] })

    fireEvent.dragEnter(target, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    fireEvent.drop(target, { dataTransfer })

    expect(localDrop).toHaveBeenCalledOnce()
    expect(onFiles).not.toHaveBeenCalled()
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()

    fireEvent.dragEnter(target, { dataTransfer })
    fireEvent.dragEnter(target.firstChild as Node, { dataTransfer })
    fireEvent.dragLeave(target.firstChild as Node, { dataTransfer })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    fireEvent.dragLeave(target, { dataTransfer })
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })

  it('prevents browser navigation for external Files even without a selected target', () => {
    render(<AgentPromptDropProvider><div>No target</div></AgentPromptDropProvider>)

    expect(documentDrop(dragData({
      files: [image()],
      types: ['Files', 'text/uri-list'],
    }))).toBe(false)
  })

  it('does not activate or intercept dragover when the target is full', () => {
    render(
      <AgentPromptDropProvider>
        <TargetHarness name="full" onFiles={vi.fn()} remainingCapacity={0} />
      </AgentPromptDropProvider>,
    )
    const dataTransfer = dragData({ files: [image()] })

    fireEvent.dragEnter(document.body, { dataTransfer })

    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
    expect(fireEvent.dragOver(document.body, { dataTransfer })).toBe(true)
    expect(dataTransfer.dropEffect).toBe('none')
  })

  it('keeps registrations and document listeners stable across option changes', () => {
    const add = vi.spyOn(document, 'addEventListener')
    const remove = vi.spyOn(document, 'removeEventListener')
    const first = vi.fn()
    const updated = vi.fn()
    const second = vi.fn()
    const { rerender, unmount } = render(
      <AgentPromptDropProvider>
        <TargetHarness name="first" onFiles={first} />
        <TargetHarness name="second" onFiles={second} />
      </AgentPromptDropProvider>,
    )
    fireEvent.focus(screen.getByTestId('target-first'))
    const listenerTypes = ['dragenter', 'dragover', 'dragleave', 'drop', 'keydown']
    const additions = add.mock.calls.filter(([type]) => listenerTypes.includes(type))

    rerender(
      <AgentPromptDropProvider>
        <TargetHarness name="first" onFiles={updated} acceptedTypesLabel="图像" />
        <TargetHarness name="second" onFiles={second} />
      </AgentPromptDropProvider>,
    )
    const file = image()
    fireEvent.dragEnter(document.body, { dataTransfer: dragData({ files: [file] }) })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    documentDrop(dragData({ files: [file] }))

    expect(updated).toHaveBeenCalledWith([file])
    expect(first).not.toHaveBeenCalled()
    expect(add.mock.calls.filter(([type]) => listenerTypes.includes(type))).toHaveLength(additions.length)

    fireEvent.dragEnter(document.body, { dataTransfer: dragData({ files: [file] }) })
    expect(screen.getByTestId('agent-prompt-drop-overlay')).toBeInTheDocument()
    unmount()
    for (const [type, listener, options] of additions) {
      if (options === undefined) {
        expect(remove).toHaveBeenCalledWith(type, listener)
      } else {
        expect(remove).toHaveBeenCalledWith(type, listener, options)
      }
    }
    expect(screen.queryByTestId('agent-prompt-drop-overlay')).not.toBeInTheDocument()
  })
})
