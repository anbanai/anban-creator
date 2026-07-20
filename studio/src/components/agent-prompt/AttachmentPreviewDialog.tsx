import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from 'react'
import {
  DownloadIcon,
  FileQuestionIcon,
  XIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { uploadsApi, type ResolveDownloadUrlRequest } from '@/lib/api/uploads'
import { downloadBlob, isDesktop, saveUrlToFile } from '@/lib/tauri'
import type {
  AgentPromptValue,
  InputAttachment,
  PromptAttachment,
} from '@/types/input-attachment'
import { classifyPromptAttachmentMetadata } from './attachment-admission'
import { ImageViewerStage } from './ImageViewerStage'

export const MAX_TEXT_PREVIEW_BYTES = 1024 * 1024
const SIGNED_URL_SAFETY_WINDOW_MS = 30_000
const TEXT_PREVIEW_TOO_LARGE = '文件过大，无法在线预览'

class TextPreviewTooLargeError extends Error {
  constructor() {
    super(TEXT_PREVIEW_TOO_LARGE)
    this.name = 'TextPreviewTooLargeError'
  }
}

export interface AttachmentPreviewOwner {
  ownerType: 'task' | 'plan'
  ownerId: string
}

export interface AttachmentPreviewDialogProps {
  open: boolean
  onOpenChange: ComponentProps<typeof Dialog>['onOpenChange']
  attachments?: readonly PromptAttachment[]
  value?: Pick<AgentPromptValue, 'attachments'>
  selectedId?: string
  selectedIndex?: number
  onSelectedChange?: (id: string, index: number) => void
  previewSource?: (id: string) => string | undefined
  sourceAttachment?: (id: string) => InputAttachment | undefined
  owner?: AttachmentPreviewOwner
  finalFocus?: ComponentProps<typeof DialogContent>['finalFocus']
}

type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'fallback'

interface SignedUrlCacheEntry {
  url?: string
  expiresAt?: number
  pending?: Promise<ResolvedRemoteSource>
}

interface ResolvedRemoteSource {
  identity: string
  url: string
  expiresAt: number
}

interface DisplaySource {
  url?: string
  remote?: ResolvedRemoteSource
}

function extension(fileName: string) {
  const dot = fileName.lastIndexOf('.')
  return dot >= 0 ? fileName.slice(dot + 1).toLowerCase() : ''
}

function previewKind(attachment: PromptAttachment): PreviewKind {
  const fileName = attachment.file?.name ?? attachment.fileName
  const contentType = attachment.file?.type || attachment.contentType || ''
  const normalizedContentType = contentType.split(';', 1)[0].trim().toLowerCase()
  const type = classifyPromptAttachmentMetadata(fileName, contentType)
  const ext = extension(fileName)
  if (type === 'image') return 'image'
  if (type === 'video') return 'video'
  if (type === 'audio') return 'audio'
  if (type === 'text' || ext === 'json' || normalizedContentType === 'application/json' || normalizedContentType === 'text/json') {
    return 'text'
  }
  if (type === 'document' && (ext === 'pdf' || normalizedContentType === 'application/pdf')) return 'pdf'
  return 'fallback'
}

function requestFor(
  attachment: PromptAttachment,
  inherited: InputAttachment | undefined,
  owner: AttachmentPreviewOwner | undefined,
): { identity: string; payload: ResolveDownloadUrlRequest } | undefined {
  const key = attachment.key ?? inherited?.key
  if (!key) return undefined
  const uploadId = attachment.uploadId ?? inherited?.upload_id
  if (uploadId) {
    return {
      identity: `upload:${uploadId}:${key}`,
      payload: { upload_id: uploadId, key },
    }
  }
  if (!owner) return undefined
  return {
    identity: `owner:${owner.ownerType}:${owner.ownerId}:${key}`,
    payload: {
      key,
      owner_type: owner.ownerType,
      owner_id: owner.ownerId,
    },
  }
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

function shouldIgnoreNavigation(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  return target.matches('input, textarea, select, [role="slider"], [data-slot="slider"], [contenteditable="true"]')
    || Boolean(target.closest('input, textarea, select, [role="slider"], [data-slot="slider"], [contenteditable="true"]'))
}

function isAuthLikeResponse(response: Response) {
  return response.status === 401 || response.status === 403 || response.status === 410
}

function readInlineTextWithLimit(text: string) {
  if (new TextEncoder().encode(text).byteLength > MAX_TEXT_PREVIEW_BYTES) {
    throw new TextPreviewTooLargeError()
  }
  return text
}

async function readBlobTextWithLimit(blob: Blob) {
  const bytes = await blob.slice(0, MAX_TEXT_PREVIEW_BYTES + 1).arrayBuffer()
  if (bytes.byteLength > MAX_TEXT_PREVIEW_BYTES) throw new TextPreviewTooLargeError()
  return new TextDecoder().decode(bytes)
}

async function readResponseTextWithLimit(response: Response) {
  const declaredLength = Number(response.headers.get('content-length'))
  if (Number.isFinite(declaredLength) && declaredLength > MAX_TEXT_PREVIEW_BYTES) {
    void response.body?.cancel()
    throw new TextPreviewTooLargeError()
  }
  if (!response.body) return readBlobTextWithLimit(await response.blob())

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let totalBytes = 0
  let text = ''
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      totalBytes += value.byteLength
      if (totalBytes > MAX_TEXT_PREVIEW_BYTES) {
        await reader.cancel()
        throw new TextPreviewTooLargeError()
      }
      text += decoder.decode(value, { stream: true })
    }
    return text + decoder.decode()
  } finally {
    reader.releaseLock()
  }
}

function MetadataFallback({ message }: { message: string }) {
  return (
    <div className="flex min-h-64 flex-1 flex-col items-center justify-center gap-3 rounded-md bg-muted/40 p-6 text-center">
      <FileQuestionIcon aria-hidden="true" className="text-muted-foreground" />
      <p className="max-w-md text-sm text-muted-foreground">{message}</p>
    </div>
  )
}

export function AttachmentPreviewDialog({
  open,
  onOpenChange,
  attachments,
  value,
  selectedId,
  selectedIndex,
  onSelectedChange,
  previewSource,
  sourceAttachment,
  owner,
  finalFocus,
}: AttachmentPreviewDialogProps) {
  const items = attachments ?? value?.attachments ?? []
  const [localIndex, setLocalIndex] = useState(0)
  const signedCacheRef = useRef(new Map<string, SignedUrlCacheEntry>())
  const rendererRetryRef = useRef(new Set<string>())
  const lastSelectedIdRef = useRef<string | undefined>(undefined)
  const activeSelectionRef = useRef<string | undefined>(undefined)
  const [displaySource, setDisplaySource] = useState<DisplaySource>({})
  const [textPreview, setTextPreview] = useState<string>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  const activeIndex = useMemo(() => {
    if (selectedId !== undefined) {
      const found = items.findIndex((item) => item.id === selectedId)
      return found >= 0 ? found : 0
    }
    if (selectedIndex !== undefined) return Math.max(0, Math.min(selectedIndex, Math.max(0, items.length - 1)))
    return Math.max(0, Math.min(localIndex, Math.max(0, items.length - 1)))
  }, [items, localIndex, selectedId, selectedIndex])
  const selected = items[activeIndex]
  const kind = selected ? previewKind(selected) : 'fallback'
  activeSelectionRef.current = open ? selected?.id : undefined

  const resolveRemote = useCallback(async (
    attachment: PromptAttachment,
    inherited: InputAttachment | undefined,
    force = false,
  ) => {
    const request = requestFor(attachment, inherited, owner)
    if (!request) return undefined
    const cached = signedCacheRef.current.get(request.identity)
    if (!force && cached?.pending) return cached.pending
    if (
      !force
      && cached?.url
      && cached.expiresAt !== undefined
      && Date.now() + SIGNED_URL_SAFETY_WINDOW_MS < cached.expiresAt
    ) {
      return { identity: request.identity, url: cached.url, expiresAt: cached.expiresAt }
    }

    const pending = uploadsApi.resolveDownloadUrl(request.payload).then((result) => {
      const resolved = {
        identity: request.identity,
        url: result.url,
        expiresAt: new Date(result.expires_at).getTime(),
      }
      signedCacheRef.current.set(request.identity, resolved)
      return resolved
    }).catch((cause) => {
      signedCacheRef.current.delete(request.identity)
      throw cause
    })
    signedCacheRef.current.set(request.identity, { pending })
    return pending
  }, [owner])

  const choose = useCallback((index: number) => {
    const bounded = Math.max(0, Math.min(index, items.length - 1))
    if (bounded === activeIndex || !items[bounded]) return
    setLocalIndex(bounded)
    onSelectedChange?.(items[bounded].id, bounded)
  }, [activeIndex, items, onSelectedChange])

  useEffect(() => {
    if (!open || !selected) return undefined
    const abortController = new AbortController()
    let active = true
    const inherited = sourceAttachment?.(selected.id)
    const localUrl = selected.file ? previewSource?.(selected.id) : undefined
    const textSize = inherited?.size ?? selected.size

    if (lastSelectedIdRef.current !== selected.id) {
      rendererRetryRef.current.delete(selected.id)
      lastSelectedIdRef.current = selected.id
    }
    setDisplaySource({})
    setTextPreview(undefined)
    setError(undefined)
    setLoading(false)

    if (kind === 'fallback') return () => abortController.abort()
    if (kind === 'text' && textSize > MAX_TEXT_PREVIEW_BYTES) {
      setError(TEXT_PREVIEW_TOO_LARGE)
      return () => abortController.abort()
    }
    if (kind === 'text' && !selected.file && inherited?.text !== undefined) {
      try {
        setTextPreview(readInlineTextWithLimit(inherited.text))
      } catch (cause) {
        setError(errorMessage(cause, '附件预览加载失败'))
      }
      return () => abortController.abort()
    }

    const load = async () => {
      try {
        setLoading(true)
        if (kind === 'text' && selected.file) {
          const text = await readBlobTextWithLimit(selected.file)
          if (active) setTextPreview(text)
          return
        }
        if (localUrl) {
          if (active) setDisplaySource({ url: localUrl })
          return
        }
        let remote = await resolveRemote(selected, inherited)
        if (!remote) return
        if (kind === 'text') {
          let refreshed = false
          for (;;) {
            const response = await fetch(remote.url, { signal: abortController.signal })
            if (response.ok) {
              const text = await readResponseTextWithLimit(response)
              if (active) setTextPreview(text)
              return
            }
            if (!refreshed && (isAuthLikeResponse(response) || Date.now() >= remote.expiresAt)) {
              refreshed = true
              const fresh = await resolveRemote(selected, inherited, true)
              if (!fresh) throw new Error('没有可用的附件来源')
              remote = fresh
              continue
            }
            throw new Error(`文本预览加载失败 (${response.status})`)
          }
        }
        if (active) setDisplaySource({ url: remote.url, remote })
      } catch (cause) {
        if (active && !abortController.signal.aborted) setError(errorMessage(cause, '附件预览加载失败'))
      } finally {
        if (active) setLoading(false)
      }
    }
    void load()
    return () => {
      active = false
      abortController.abort()
    }
  }, [kind, open, previewSource, resolveRemote, selected, sourceAttachment])

  useEffect(() => {
    if (!open) return undefined
    const handleKeyDown = (event: KeyboardEvent) => {
      if (shouldIgnoreNavigation(event.target)) return
      if (event.key === 'ArrowLeft') choose(activeIndex - 1)
      if (event.key === 'ArrowRight') choose(activeIndex + 1)
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [activeIndex, choose, open])

  const handleRendererError = useCallback(() => {
    if (!selected || !displaySource.remote || rendererRetryRef.current.has(selected.id)) {
      setError('预览加载失败，请下载后查看')
      return
    }
    rendererRetryRef.current.add(selected.id)
    const refreshId = selected.id
    setLoading(true)
    setError(undefined)
    const inherited = sourceAttachment?.(selected.id)
    void resolveRemote(selected, inherited, true).then((remote) => {
      if (!remote) throw new Error('没有可用的附件来源')
      if (activeSelectionRef.current !== refreshId) return
      setDisplaySource({ url: remote.url, remote })
    }).catch((cause) => {
      if (activeSelectionRef.current !== refreshId) return
      setError(errorMessage(cause, '预览加载失败，请下载后查看'))
    }).finally(() => {
      if (activeSelectionRef.current === refreshId) setLoading(false)
    })
  }, [displaySource.remote, resolveRemote, selected, sourceAttachment])

  const downloadRemote = useCallback(async (
    attachment: PromptAttachment,
    inherited: InputAttachment | undefined,
  ) => {
    let refreshed = false
    for (;;) {
      const remote = await resolveRemote(attachment, inherited, refreshed)
      if (!remote) throw new Error('没有可下载的附件来源')
      if (isDesktop()) {
        try {
          if (await saveUrlToFile(remote.url, attachment.fileName)) return
        } catch {
          // Fall through so a signed-URL status can be inspected and refreshed.
        }
      }
      let response: Response
      try {
        response = await fetch(remote.url)
      } catch (cause) {
        if (!refreshed) {
          refreshed = true
          continue
        }
        throw cause
      }
      if (!response.ok) {
        if (!refreshed && (isAuthLikeResponse(response) || Date.now() >= remote.expiresAt)) {
          refreshed = true
          continue
        }
        throw new Error(`附件下载失败 (${response.status})`)
      }
      await downloadBlob(attachment.fileName, await response.blob())
      return
    }
  }, [resolveRemote])

  const handleDownload = useCallback(async () => {
    if (!selected) return
    setError(undefined)
    try {
      if (selected.file) {
        await downloadBlob(selected.fileName, selected.file)
        return
      }
      const inherited = sourceAttachment?.(selected.id)
      if (inherited?.text !== undefined) {
        await downloadBlob(
          selected.fileName,
          new Blob([inherited.text], { type: selected.contentType || 'text/plain' }),
        )
        return
      }
      await downloadRemote(selected, inherited)
    } catch (cause) {
      setError(errorMessage(cause, '附件下载失败'))
    }
  }, [downloadRemote, selected, sourceAttachment])

  let renderer: ReactNode
  if (!selected) {
    renderer = <MetadataFallback message="没有可预览的附件" />
  } else if (error) {
    renderer = <MetadataFallback message={error} />
  } else if (loading) {
    renderer = <MetadataFallback message="正在加载预览…" />
  } else if (kind === 'text' && textPreview !== undefined) {
    renderer = (
      <pre
        data-testid="attachment-text-preview"
        className="max-h-[62vh] min-h-64 w-full overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/40 p-4 font-mono text-xs leading-relaxed"
      >
        {textPreview}
      </pre>
    )
  } else if (kind === 'image' && displaySource.url) {
    renderer = (
      <ImageViewerStage
        src={displaySource.url}
        alt={selected.fileName}
        index={activeIndex}
        count={items.length}
        onPrevious={() => choose(activeIndex - 1)}
        onNext={() => choose(activeIndex + 1)}
        onError={handleRendererError}
        lightbox
      />
    )
  } else if (kind === 'video' && displaySource.url) {
    renderer = <video src={displaySource.url} controls playsInline preload="metadata" onError={handleRendererError} className="max-h-[62vh] max-w-full" />
  } else if (kind === 'audio' && displaySource.url) {
    renderer = <audio src={displaySource.url} controls preload="metadata" onError={handleRendererError} className="w-full max-w-xl" />
  } else if (kind === 'pdf' && displaySource.url) {
    renderer = (
      <object data={displaySource.url} type="application/pdf" aria-label={selected.fileName} onError={handleRendererError} className="min-h-[60vh] w-full">
        <MetadataFallback message="PDF 无法在线显示，请下载后查看" />
      </object>
    )
  } else if (kind === 'fallback') {
    renderer = <MetadataFallback message="此文件类型不支持在线预览" />
  } else {
    renderer = <MetadataFallback message="没有可用的授权预览来源" />
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        finalFocus={finalFocus}
        className="top-0 left-0 flex h-dvh w-screen max-w-none min-w-0 translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none bg-black/95 p-0 text-white ring-0 sm:max-w-none"
      >
        <DialogHeader className="absolute inset-x-0 top-0 z-20 flex-row items-center justify-end gap-2 p-3">
          <DialogTitle className="sr-only">
            {selected?.fileName ?? '附件预览'}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {selected ? `${activeIndex + 1} / ${items.length} · ${selected.contentType || selected.type}` : '附件详情'}
          </DialogDescription>
          {selected ? (
            <Button type="button" variant="secondary" size="icon" onClick={handleDownload} aria-label={`下载 ${selected.fileName}`} className="rounded-full bg-white text-black hover:bg-white/85">
              <DownloadIcon />
            </Button>
          ) : null}
          <Tooltip>
            <TooltipTrigger
              render={
                <DialogClose
                  render={
                    <Button type="button" variant="secondary" size="icon" aria-label="关闭附件预览" className="rounded-full bg-white text-black hover:bg-white/85" />
                  }
                />
              }
            >
              <XIcon data-icon="inline-start" />
            </TooltipTrigger>
            <TooltipContent>关闭</TooltipContent>
          </Tooltip>
        </DialogHeader>

        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex min-h-0 min-w-0 flex-1 items-center justify-center">{renderer}</div>
          {error ? <p role="alert" className="sr-only">{error}</p> : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
