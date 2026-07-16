import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FocusEventHandler,
  type ReactNode,
  type RefCallback,
} from 'react'
import { createPortal } from 'react-dom'
import { UploadIcon } from 'lucide-react'

export interface AgentPromptDropTargetOptions {
  enabled: boolean
  remainingCapacity: number
  acceptedTypesLabel: string
  acceptsFile: (file: File) => boolean
  acceptsItemType?: (mime: string) => boolean
  onFiles: (files: File[]) => void
}

interface DropTargetRegistration {
  id: string
  elementRef: React.RefObject<HTMLElement | null>
  optionsRef: React.RefObject<AgentPromptDropTargetOptions>
  focusedAt: number
}

interface DropTargetRegistry {
  register: (registration: DropTargetRegistration) => () => void
  focus: (id: string) => void
  refresh: (id: string) => void
}

interface OverlayState {
  acceptedTypesLabel: string
}

const OPEN_OVERLAY_SELECTOR = [
  '[data-slot="dialog-content"][data-open]',
  '[data-slot="alert-dialog-content"][data-open]',
].join(',')

const DropTargetContext = createContext<DropTargetRegistry | null>(null)

function isFilesTransfer(dataTransfer: DataTransfer | null): dataTransfer is DataTransfer {
  if (!dataTransfer) return false
  const types = Array.from(dataTransfer.types ?? [])
  return types.length === 1 && types[0] === 'Files'
}

function isElementAvailable(element: HTMLElement | null) {
  if (!element?.isConnected) return false
  if (element.closest('[hidden], [aria-hidden="true"]')) return false
  const style = window.getComputedStyle(element)
  return style.display !== 'none' && style.visibility !== 'hidden'
}

function isPotentiallyAccepted(
  dataTransfer: DataTransfer,
  options: AgentPromptDropTargetOptions,
) {
  const items = Array.from(dataTransfer.items ?? []).filter((item) => item.kind === 'file')
  if (items.length === 0) return true
  if (items.some((item) => item.type === '')) return true
  if (!options.acceptsItemType) return true
  return items.some((item) => options.acceptsItemType?.(item.type) === true)
}

function latestOpenOverlay() {
  const overlays = document.querySelectorAll<HTMLElement>(OPEN_OVERLAY_SELECTOR)
  return overlays.item(overlays.length - 1) || null
}

export function AgentPromptDropProvider({ children }: { children: ReactNode }) {
  const registrationsRef = useRef(new Map<string, DropTargetRegistration>())
  const focusSequenceRef = useRef(0)
  const dragDepthRef = useRef(0)
  const dragTransferRef = useRef<DataTransfer | null>(null)
  const activeTargetIdRef = useRef<string | null>(null)
  const activeLabelRef = useRef<string | null>(null)
  const [overlay, setOverlay] = useState<OverlayState | null>(null)

  const clearOverlay = useCallback(() => {
    activeTargetIdRef.current = null
    activeLabelRef.current = null
    setOverlay((current) => current === null ? current : null)
  }, [])

  const resetDrag = useCallback(() => {
    dragDepthRef.current = 0
    dragTransferRef.current = null
    clearOverlay()
  }, [clearOverlay])

  const selectTarget = useCallback((dataTransfer: DataTransfer) => {
    const topOverlay = latestOpenOverlay()
    if (topOverlay?.dataset.slot === 'alert-dialog-content') return null

    const eligible = [...registrationsRef.current.values()].filter((registration) => {
      const element = registration.elementRef.current
      const options = registration.optionsRef.current
      return isElementAvailable(element)
        && options.enabled
        && options.remainingCapacity > 0
        && isPotentiallyAccepted(dataTransfer, options)
        && (!topOverlay || topOverlay.contains(element))
    })

    let lastFocused: DropTargetRegistration | null = null
    for (const registration of eligible) {
      if (registration.focusedAt > (lastFocused?.focusedAt ?? 0)) {
        lastFocused = registration
      }
    }
    if (lastFocused) return lastFocused
    return eligible.length === 1 ? eligible[0] : null
  }, [])

  const activateTarget = useCallback((registration: DropTargetRegistration) => {
    const acceptedTypesLabel = registration.optionsRef.current.acceptedTypesLabel
    if (
      activeTargetIdRef.current === registration.id
      && activeLabelRef.current === acceptedTypesLabel
    ) return

    activeTargetIdRef.current = registration.id
    activeLabelRef.current = acceptedTypesLabel
    setOverlay({ acceptedTypesLabel })
  }, [])

  const register = useCallback((registration: DropTargetRegistration) => {
    registrationsRef.current.set(registration.id, registration)
    return () => {
      registrationsRef.current.delete(registration.id)
      if (activeTargetIdRef.current === registration.id) resetDrag()
    }
  }, [resetDrag])

  const focus = useCallback((id: string) => {
    const registration = registrationsRef.current.get(id)
    if (!registration) return
    focusSequenceRef.current += 1
    registration.focusedAt = focusSequenceRef.current
  }, [])

  const refresh = useCallback((id: string) => {
    if (activeTargetIdRef.current !== id) return
    const dataTransfer = dragTransferRef.current
    if (!dataTransfer) {
      resetDrag()
      return
    }
    const selected = selectTarget(dataTransfer)
    if (selected?.id !== id) {
      resetDrag()
      return
    }
    activateTarget(selected)
  }, [activateTarget, resetDrag, selectTarget])

  const registry = useMemo<DropTargetRegistry>(() => ({
    register,
    focus,
    refresh,
  }), [focus, refresh, register])

  useEffect(() => {
    const onDragEnter = (event: DragEvent) => {
      if (!isFilesTransfer(event.dataTransfer)) return
      dragDepthRef.current += 1
      dragTransferRef.current = event.dataTransfer
      const selected = selectTarget(event.dataTransfer)
      if (selected) {
        activateTarget(selected)
      } else {
        clearOverlay()
      }
    }

    const onDragOver = (event: DragEvent) => {
      if (!isFilesTransfer(event.dataTransfer)) return
      dragTransferRef.current = event.dataTransfer
      const selected = selectTarget(event.dataTransfer)
      if (!selected) {
        clearOverlay()
        return
      }
      activateTarget(selected)
      event.preventDefault()
      event.dataTransfer.dropEffect = 'copy'
    }

    const onDragLeave = () => {
      if (dragDepthRef.current === 0) return
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1)
      if (dragDepthRef.current === 0) resetDrag()
    }

    const onDrop = (event: DragEvent) => {
      const wasDefaultPrevented = event.defaultPrevented
      if (!isFilesTransfer(event.dataTransfer)) return

      event.preventDefault()
      const selected = selectTarget(event.dataTransfer)
      const files = Array.from(event.dataTransfer.files ?? [])
      resetDrag()

      if (wasDefaultPrevented || !selected || files.length === 0) return
      const options = selected.optionsRef.current
      if (!files.some((file) => options.acceptsFile(file))) return
      options.onFiles(files)
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') resetDrag()
    }

    document.addEventListener('dragenter', onDragEnter)
    document.addEventListener('dragover', onDragOver)
    document.addEventListener('dragleave', onDragLeave)
    document.addEventListener('drop', onDrop)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('dragenter', onDragEnter)
      document.removeEventListener('dragover', onDragOver)
      document.removeEventListener('dragleave', onDragLeave)
      document.removeEventListener('drop', onDrop)
      document.removeEventListener('keydown', onKeyDown)
      resetDrag()
      registrationsRef.current.clear()
    }
  }, [activateTarget, clearOverlay, resetDrag, selectTarget])

  const overlayStatus = overlay
    ? `释放以添加${overlay.acceptedTypesLabel}`
    : ''

  return (
    <DropTargetContext.Provider value={registry}>
      {children}
      {overlay && createPortal(
        <>
          <div
            data-testid="agent-prompt-drop-overlay"
            aria-hidden="true"
            className="pointer-events-none fixed inset-0 isolate z-50 flex items-center justify-center p-4"
          >
            <div className="flex min-h-40 w-full max-w-xl items-center justify-center rounded-lg border-2 border-dashed border-primary bg-background/90 px-6 py-10 shadow-lg backdrop-blur-sm">
              <div className="flex flex-col items-center gap-3 text-center text-foreground">
                <UploadIcon className="size-8 text-primary" />
                <p className="text-base font-medium">{overlayStatus}</p>
              </div>
            </div>
          </div>
          <div
            role="status"
            aria-live="polite"
            aria-atomic="true"
            className="sr-only"
          >
            {overlayStatus}
          </div>
        </>,
        document.body,
      )}
    </DropTargetContext.Provider>
  )
}

export function useAgentPromptDropTarget(options: AgentPromptDropTargetOptions) {
  const registry = useContext(DropTargetContext)
  if (!registry) {
    throw new Error('useAgentPromptDropTarget must be used within AgentPromptDropProvider')
  }

  const id = useId()
  const elementRef = useRef<HTMLElement | null>(null)
  const optionsRef = useRef(options)
  optionsRef.current = options

  const ref = useCallback<RefCallback<HTMLElement>>((element) => {
    elementRef.current = element
  }, [])

  useLayoutEffect(() => registry.register({
    id,
    elementRef,
    optionsRef,
    focusedAt: 0,
  }), [id, registry])

  useLayoutEffect(() => {
    registry.refresh(id)
  })

  const onFocusCapture = useCallback<FocusEventHandler<HTMLElement>>(() => {
    registry.focus(id)
  }, [id, registry])

  return { ref, onFocusCapture }
}
