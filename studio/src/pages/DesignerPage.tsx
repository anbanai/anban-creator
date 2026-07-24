import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { PaintbrushIcon, SendIcon, WalletCardsIcon, XIcon } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import DesignerToolbar, { type DesignerSettingsPatch } from '@/components/designer/DesignerToolbar'
import DesignerCanvas from '@/components/designer/DesignerCanvas'
import InlineMaskEditor from '@/components/designer/InlineMaskEditor'
import HistoryDrawer from '@/components/designer/HistoryDrawer'
import ImagePreview from '@/components/designer/ImagePreview'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { designerApi } from '@/lib/api/designer'
import { uploadToOSS } from '@/lib/direct-upload'
import { getApiErrorCode, getApiErrorMessage, sanitizeUserFacingErrorMessage } from '@/lib/http-client'
import { buildDesignerRequestSize } from '@/lib/designer-size'
import { clearActiveGeneration, loadActiveGeneration, saveActiveGeneration } from '@/lib/designer-session'
import {
  AttachmentRejectionReason,
  type AgentPromptValue,
  type AttachmentAdmissionPolicy,
  type AttachmentRejection,
} from '@/types/input-attachment'
import type { DesignerProvider, DesignerSettings, GenerateImage, ImageGeneration, ImageGenerationResult } from '@/types/designer'
import type { InlineMaskEditorHandle } from '@/components/designer/InlineMaskEditor'

const DEFAULT_SETTINGS: DesignerSettings = {
  quality: 'auto',
  size: 'auto',
  resolution: '2K',
  n: 1,
  outputFormat: 'png',
  compression: 100,
  background: 'auto',
  watermark: false,
}

const EMPTY_PROMPT_VALUE: AgentPromptValue = { prompt: '', attachments: [] }
const MAX_DESIGNER_REFERENCE_BYTES = 10 * 1024 * 1024
const POLL_INTERVAL = 2000
const MAX_POLLS = 180
const MAX_CONSECUTIVE_ERRORS = 5

function maxReferenceImagesForProvider(provider?: DesignerProvider): number {
  const capabilities = provider?.capabilities
  return capabilities?.supportsReference
    ? Math.max(0, capabilities.maxReferenceImages)
    : 0
}

function resultsToImages(results?: ImageGenerationResult[]): GenerateImage[] {
  if (!results?.length) return []
  return results
    .filter((result) => result.image_url || result.image_path)
    .map((result, index) => ({
      url: result.image_url || result.image_path!,
      width: result.width,
      height: result.height,
      index,
    }))
}

function describeAttachmentRejections(rejections: readonly AttachmentRejection[]): string {
  const counts = new Map<AttachmentRejectionReason, number>()
  for (const rejection of rejections) {
    counts.set(rejection.reason, (counts.get(rejection.reason) ?? 0) + 1)
  }
  const labels: Record<AttachmentRejectionReason, string> = {
    [AttachmentRejectionReason.Duplicate]: '重复文件',
    [AttachmentRejectionReason.UnsupportedType]: '不支持的文件',
    [AttachmentRejectionReason.TooLarge]: '超过大小限制的文件',
    [AttachmentRejectionReason.Capacity]: '超过数量上限的文件',
  }
  return [...counts].map(([reason, count]) => `忽略 ${count} 个${labels[reason]}`).join('，')
}

interface GenerationAttempt {
  id: number
  signal: AbortSignal
}

function awaitGenerationAttempt<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(new DOMException('Generation canceled', 'AbortError'))
  return new Promise<T>((resolve, reject) => {
    const handleAbort = () => reject(new DOMException('Generation canceled', 'AbortError'))
    signal.addEventListener('abort', handleAbort, { once: true })
    promise.then(resolve, reject).finally(() => {
      signal.removeEventListener('abort', handleAbort)
    })
  })
}

export default function DesignerPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(null)
  const [settings, setSettings] = useState<DesignerSettings>(DEFAULT_SETTINGS)
  const [promptValue, setPromptValueState] = useState<AgentPromptValue>(EMPTY_PROMPT_VALUE)
  const promptValueRef = useRef(promptValue)
  const [currentImages, setCurrentImages] = useState<GenerateImage[]>([])
  const [currentGeneration, setCurrentGeneration] = useState<ImageGeneration | null>(null)
  const [selectedGenerationId, setSelectedGenerationId] = useState<string>()
  const [previewImage, setPreviewImage] = useState<string | null>(null)
  const [isGenerating, setIsGenerating] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [editingImage, setEditingImage] = useState<GenerateImage | null>(null)
  const maskEditorRef = useRef<InlineMaskEditorHandle>(null)
  const pollingRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortedRef = useRef(false)
  const generationAttemptRef = useRef(0)
  const generationAbortRef = useRef<AbortController | null>(null)
  const normalizedProviderIdRef = useRef('')
  const normalizedReferenceCapacityRef = useRef(0)

  const setPromptValue = useCallback((next: AgentPromptValue | ((current: AgentPromptValue) => AgentPromptValue)) => {
    setPromptValueState((current) => {
      const resolved = typeof next === 'function' ? next(current) : next
      promptValueRef.current = resolved
      return resolved
    })
  }, [])

  const { data: providers } = useQuery({
    queryKey: ['designer', 'providers'],
    queryFn: () => designerApi.getProviders(),
  })
  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ['projects', 'designer', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
  })
  const { data: wallet, isError: walletError } = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })

  const providerList: DesignerProvider[] = providers ?? []
  const activeProvider = providerList.find((provider) => provider.id === selectedProviderId && provider.enabled)
  const effectiveProvider = activeProvider ?? providerList.find((provider) => provider.enabled)
  const hasInsufficientCredits = Boolean(
    !walletError && wallet && effectiveProvider && wallet.balance < effectiveProvider.credits,
  )
  const effectiveCaps = effectiveProvider?.capabilities
  const maxReferenceImages = maxReferenceImagesForProvider(effectiveProvider)
  const referenceCapacity = Math.min(maxReferenceImages, GENERAL_AGENT_ATTACHMENT_POLICY.maxCount)
  const projectID = selectedProjectId ?? 'default'
  const attachmentPolicy = useMemo<AttachmentAdmissionPolicy>(() => ({
    allowedTypes: ['image'],
    maxCount: editingImage ? 0 : referenceCapacity,
    maxBytes: { image: MAX_DESIGNER_REFERENCE_BYTES },
  }), [editingImage, referenceCapacity])

  const attachmentController = usePromptAttachments({
    adapter: { mode: 'designer' },
    policy: attachmentPolicy,
    attachments: promptValue.attachments,
    onAttachmentsChange: (attachments) => {
      setPromptValue((current) => ({ ...current, attachments }))
    },
  })

  const updateSettings = useCallback((patch: DesignerSettingsPatch) => {
    setSettings((current) => ({ ...current, ...patch }))
  }, [])

  const resetSettingsForProvider = useCallback((provider?: DesignerProvider) => {
    const capabilities = provider?.capabilities
    setSettings({
      ...DEFAULT_SETTINGS,
      size: capabilities?.defaultSize || DEFAULT_SETTINGS.size,
      quality: capabilities?.qualityLevels?.[0] ?? DEFAULT_SETTINGS.quality,
      n: Math.min(DEFAULT_SETTINGS.n, Math.max(1, capabilities?.maxBatch ?? 1)),
    })
  }, [])

  const startGenerationAttempt = useCallback(() => {
    generationAbortRef.current?.abort()
    const controller = new AbortController()
    generationAbortRef.current = controller
    generationAttemptRef.current += 1
    abortedRef.current = false
    return { id: generationAttemptRef.current, signal: controller.signal }
  }, [])

  const isGenerationAttemptActive = useCallback((attempt: GenerationAttempt) => (
    generationAttemptRef.current === attempt.id
    && !attempt.signal.aborted
    && !abortedRef.current
  ), [])

  const clearSubmittedPrompt = useCallback((submittedPrompt: string) => {
    setPromptValue((current) => (
      current.prompt === submittedPrompt ? { ...current, prompt: '' } : current
    ))
  }, [setPromptValue])

  useEffect(() => {
    if (providers === undefined) return
    const effectiveProviderID = effectiveProvider?.id ?? ''
    if (selectedProviderId !== effectiveProviderID) setSelectedProviderId(effectiveProviderID)

    const providerChanged = normalizedProviderIdRef.current !== effectiveProviderID
    const capacityChanged = normalizedReferenceCapacityRef.current !== referenceCapacity
    if (!providerChanged && !capacityChanged) return
    normalizedProviderIdRef.current = effectiveProviderID
    normalizedReferenceCapacityRef.current = referenceCapacity
    if (providerChanged) resetSettingsForProvider(effectiveProvider)

    const overflow = promptValueRef.current.attachments.slice(referenceCapacity)
    for (const attachment of overflow) attachmentController.remove(attachment.id)
    if (overflow.length > 0) {
      toast.warning(`当前输入最多支持 ${referenceCapacity} 张参考图，已移除 ${overflow.length} 张`)
    }
  }, [attachmentController, effectiveProvider, providers, referenceCapacity, resetSettingsForProvider, selectedProviderId])

  useEffect(() => () => {
    generationAttemptRef.current += 1
    abortedRef.current = true
    generationAbortRef.current?.abort()
    generationAbortRef.current = null
    if (pollingRef.current) clearTimeout(pollingRef.current)
  }, [])

  const stopPolling = useCallback(() => {
    if (!pollingRef.current) return
    clearTimeout(pollingRef.current)
    pollingRef.current = null
  }, [])

  const finalizeGenerationAttempt = useCallback((attempt: GenerationAttempt) => {
    if (!isGenerationAttemptActive(attempt)) return false
    generationAttemptRef.current += 1
    abortedRef.current = true
    if (generationAbortRef.current?.signal === attempt.signal) {
      generationAbortRef.current.abort()
      generationAbortRef.current = null
    }
    stopPolling()
    return true
  }, [isGenerationAttemptActive, stopPolling])

  const startPolling = useCallback((generationID: string, attempt: GenerationAttempt) => {
    if (!isGenerationAttemptActive(attempt)) return
    stopPolling()
    let pollCount = 0
    let consecutiveErrors = 0

    const scheduleNext = () => {
      if (!isGenerationAttemptActive(attempt)) return
      pollingRef.current = setTimeout(() => { void poll() }, POLL_INTERVAL)
    }

    const poll = async () => {
      if (!isGenerationAttemptActive(attempt)) return
      pollCount += 1
      if (pollCount >= MAX_POLLS) {
        if (!finalizeGenerationAttempt(attempt)) return
        setIsGenerating(false)
        clearActiveGeneration()
        toast.error('生成超时，请稍后在历史记录中查看结果')
        return
      }
      try {
        const generation = await awaitGenerationAttempt(
          designerApi.getGeneration(generationID, attempt.signal),
          attempt.signal,
        )
        if (!isGenerationAttemptActive(attempt)) return
        consecutiveErrors = 0
        if (generation.status === 'completed') {
          if (!finalizeGenerationAttempt(attempt)) return
          setIsGenerating(false)
          setCurrentGeneration(generation)
          setCurrentImages(resultsToImages(generation.results))
          clearActiveGeneration()
          toast.success('图片生成成功')
          void queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
        } else if (generation.status === 'failed') {
          if (!finalizeGenerationAttempt(attempt)) return
          setIsGenerating(false)
          clearActiveGeneration()
          toast.error(sanitizeUserFacingErrorMessage(generation.error, '图片生成失败，请稍后重试'))
        } else {
          scheduleNext()
        }
      } catch {
        if (!isGenerationAttemptActive(attempt)) return
        consecutiveErrors += 1
        if (consecutiveErrors >= MAX_CONSECUTIVE_ERRORS) {
          if (!finalizeGenerationAttempt(attempt)) return
          setIsGenerating(false)
          clearActiveGeneration()
          toast.error('查询生成状态失败')
        } else {
          scheduleNext()
        }
      }
    }

    scheduleNext()
  }, [finalizeGenerationAttempt, isGenerationAttemptActive, queryClient, stopPolling])

  useEffect(() => {
    const active = loadActiveGeneration()
    if (!active) return
    const attempt = startGenerationAttempt()
    awaitGenerationAttempt(
      designerApi.getGeneration(active.generationId, attempt.signal),
      attempt.signal,
    ).then((generation) => {
      if (!isGenerationAttemptActive(attempt)) return
      if (generation.status === 'completed') {
        if (!finalizeGenerationAttempt(attempt)) return
        setCurrentGeneration(generation)
        setCurrentImages(resultsToImages(generation.results))
        setSelectedGenerationId(active.generationId)
        clearActiveGeneration()
      } else if (generation.status === 'failed') {
        if (!finalizeGenerationAttempt(attempt)) return
        toast.error(sanitizeUserFacingErrorMessage(generation.error, '图片生成失败，请稍后重试'))
        clearActiveGeneration()
      } else {
        setSelectedGenerationId(active.generationId)
        setIsGenerating(true)
        startPolling(active.generationId, attempt)
      }
    }).catch(() => {
      if (isGenerationAttemptActive(attempt)) clearActiveGeneration()
    })
  }, [finalizeGenerationAttempt, isGenerationAttemptActive, startGenerationAttempt, startPolling])

  const beginGeneration = useCallback((generationID: string, attempt: GenerationAttempt) => {
    if (!isGenerationAttemptActive(attempt)) return
    setSelectedGenerationId(generationID)
    saveActiveGeneration(generationID)
    startPolling(generationID, attempt)
  }, [isGenerationAttemptActive, startPolling])

  const showGenerationError = useCallback((error: unknown) => {
    const message = getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')
    if (getApiErrorCode(error) === 40203) {
      toast.error(message, {
        action: { label: '去充值', onClick: () => navigate('/billing') },
      })
      return
    }
    toast.error(message)
  }, [navigate])

  const handleGenerate = useCallback(async (value: AgentPromptValue) => {
    if (!effectiveProvider) {
      toast.error('没有可用的图片模型')
      return
    }
    const attempt = startGenerationAttempt()
    const submittedPrompt = value.prompt
    stopPolling()
    setIsGenerating(true)
    setCurrentGeneration(null)
    setCurrentImages([])
    try {
      const supportedReferences = value.attachments.slice(0, referenceCapacity)
      const referenceFileIDs: string[] = []
      for (const attachment of supportedReferences) {
        if (!attachment.uploadId || !attachment.key || attachment.status !== 'uploaded') {
          throw new Error('参考图仍在上传或上传失败')
        }
        const registered = await awaitGenerationAttempt(
          designerApi.registerReference({
            upload_id: attachment.uploadId,
            key: attachment.key,
          }, attempt.signal),
          attempt.signal,
        )
        if (!isGenerationAttemptActive(attempt)) return
        referenceFileIDs.push(registered.file_id)
      }
      const { generation_id } = await awaitGenerationAttempt(
        designerApi.generate({
          project_id: projectID,
          prompt: value.prompt.trim(),
          provider: effectiveProvider.provider,
          provider_id: effectiveProvider.id,
          quality: settings.quality !== 'auto' ? settings.quality : undefined,
          size: buildDesignerRequestSize(settings.size, settings.resolution),
          n: settings.n > 1 ? settings.n : undefined,
          output_format: settings.outputFormat !== 'png' ? settings.outputFormat : undefined,
          output_compression: effectiveCaps?.hasCompression && settings.compression < 100 ? settings.compression : undefined,
          background: effectiveCaps?.hasBackground && settings.background !== 'auto' ? settings.background : undefined,
          reference_file_ids: referenceFileIDs.length > 0 ? referenceFileIDs : undefined,
          watermark: settings.watermark || undefined,
        }, attempt.signal),
        attempt.signal,
      )
      if (!isGenerationAttemptActive(attempt)) return
      clearSubmittedPrompt(submittedPrompt)
      beginGeneration(generation_id, attempt)
    } catch (error) {
      if (!isGenerationAttemptActive(attempt)) return
      setIsGenerating(false)
      showGenerationError(error)
    }
  }, [beginGeneration, clearSubmittedPrompt, effectiveCaps, effectiveProvider, isGenerationAttemptActive, projectID, referenceCapacity, settings, showGenerationError, startGenerationAttempt, stopPolling])

  const handleEditSubmit = useCallback(async (value: AgentPromptValue) => {
    if (!effectiveProvider || !editingImage) return
    const attempt = startGenerationAttempt()
    const submittedPrompt = value.prompt
    setIsGenerating(true)
    try {
      const maskFile = await awaitGenerationAttempt(
        Promise.resolve(maskEditorRef.current?.exportMask()).then((result) => result ?? null),
        attempt.signal,
      )
      if (!isGenerationAttemptActive(attempt)) return
      if (!maskFile) {
        setIsGenerating(false)
        toast.error('请先涂抹需要编辑的区域')
        return
      }
      stopPolling()
      const sourceImage = editingImage
      setEditingImage(null)
      const [source, maskUpload] = await awaitGenerationAttempt(
        Promise.all([
          designerApi.uploadReferenceFromUrl(sourceImage.url, attempt.signal),
          uploadToOSS({
            purpose: 'designer_reference',
            file: maskFile,
            signal: attempt.signal,
          }),
        ]),
        attempt.signal,
      )
      if (!isGenerationAttemptActive(attempt)) return
      const mask = await awaitGenerationAttempt(
        designerApi.registerReference({
          upload_id: maskUpload.uploadId,
          key: maskUpload.key,
        }, attempt.signal),
        attempt.signal,
      )
      if (!isGenerationAttemptActive(attempt)) return
      const { generation_id } = await awaitGenerationAttempt(
        designerApi.generate({
          project_id: projectID,
          prompt: value.prompt.trim(),
          provider: effectiveProvider.provider,
          provider_id: effectiveProvider.id,
          reference_file_ids: [source.file_id],
          mask_file_id: mask.file_id,
        }, attempt.signal),
        attempt.signal,
      )
      if (!isGenerationAttemptActive(attempt)) return
      clearSubmittedPrompt(submittedPrompt)
      beginGeneration(generation_id, attempt)
    } catch (error) {
      if (!isGenerationAttemptActive(attempt)) return
      setIsGenerating(false)
      showGenerationError(error)
    }
  }, [beginGeneration, clearSubmittedPrompt, editingImage, effectiveProvider, isGenerationAttemptActive, projectID, showGenerationError, startGenerationAttempt, stopPolling])

  const handleCancel = useCallback(() => {
    generationAttemptRef.current += 1
    abortedRef.current = true
    generationAbortRef.current?.abort()
    generationAbortRef.current = null
    stopPolling()
    setIsGenerating(false)
    clearActiveGeneration()
    if (editingImage) {
      setEditingImage(null)
    }
  }, [editingImage, stopPolling])

  const handleAttachmentRejected = useCallback((rejections: AttachmentRejection[]) => {
    const message = describeAttachmentRejections(rejections)
    if (message) toast.warning(message)
  }, [])

  function handleModelChange(providerID: string) {
    const provider = providerList.find((candidate) => candidate.id === providerID)
    setSelectedProviderId(providerID)
    normalizedProviderIdRef.current = providerID
    normalizedReferenceCapacityRef.current = maxReferenceImagesForProvider(provider)
    resetSettingsForProvider(provider)
    const overflow = promptValueRef.current.attachments.slice(maxReferenceImagesForProvider(provider))
    for (const attachment of overflow) attachmentController.remove(attachment.id)
    if (overflow.length > 0) {
      toast.warning(`当前模型最多支持 ${maxReferenceImagesForProvider(provider)} 张参考图，已移除 ${overflow.length} 张`)
    }
  }

  const contextBar = (
    <ProjectContextControl
      mode="select"
      projects={projects}
      value={selectedProjectId}
      onValueChange={setSelectedProjectId}
      allowNoProject
      createProjectHref="/projects"
      loading={projectsLoading}
      disabled={isGenerating}
      placeholder="选择项目"
    />
  )

  return (
    <div
      data-testid="designer-workspace"
      className="relative -mx-4 -my-6 flex h-[calc(100dvh-2.5rem)] flex-col overflow-hidden bg-background md:-mx-8 md:-my-8 md:h-dvh md:flex-row"
    >
      <DesignerToolbar
        providers={providerList}
        selectedProviderId={effectiveProvider?.id ?? ''}
        onModelChange={handleModelChange}
        capabilities={effectiveCaps}
        settings={settings}
        onSettingsChange={updateSettings}
        onHistoryToggle={() => setHistoryOpen(true)}
      />

      <div className="relative flex min-h-0 w-full min-w-0 flex-1 flex-col p-3 md:w-auto md:pl-0">
        <div
          data-testid="designer-canvas-frame"
          className="relative flex-1 overflow-hidden rounded-2xl border border-border/70 bg-card/35 shadow-inner"
          style={editingImage ? undefined : { backgroundImage: 'radial-gradient(circle, color-mix(in oklch, var(--color-border) 55%, transparent) 0.5px, transparent 0.5px)', backgroundSize: '20px 20px' }}
        >
          <div className="h-full overflow-y-auto p-4 pb-52 md:p-6 md:pb-56">
            {editingImage ? (
              <InlineMaskEditor ref={maskEditorRef} imageUrl={editingImage.url} onClose={() => setEditingImage(null)} />
            ) : (
              <DesignerCanvas
                images={currentImages}
                isGenerating={isGenerating}
                canInpaint={effectiveCaps?.supportsMask ?? false}
                onImageClick={(image) => setPreviewImage(image.url)}
                onEdit={setEditingImage}
              />
            )}
          </div>
          <div className="pointer-events-none absolute inset-x-4 bottom-4 md:inset-x-6 md:bottom-6">
            <div className="pointer-events-auto w-full">
              <AgentPromptInput
                value={promptValue}
                onChange={setPromptValue}
                onSubmit={editingImage ? handleEditSubmit : handleGenerate}
                attachmentController={attachmentController}
                attachmentPolicy={attachmentPolicy}
                contextBar={contextBar}
                placeholder={editingImage ? '描述你想修改的区域...' : '描述你想要生成的图片...'}
                submitLabel={editingImage ? '编辑' : '生成'}
                submitIcon={editingImage ? PaintbrushIcon : SendIcon}
                submitting={isGenerating}
                submitDisabled={!promptValue.prompt.trim() || !effectiveProvider || hasInsufficientCredits}
                ariaLabel="Designer prompt"
                acceptedTypesLabel="图片"
                onAttachmentRejected={handleAttachmentRejected}
                status={isGenerating ? (
                  <span className="text-xs text-muted-foreground">正在生成...</span>
                ) : effectiveProvider ? (
                  <span className="text-xs text-muted-foreground">
                    {effectiveProvider.credits.toLocaleString()} 积分
                    {wallet ? ` · 余额 ${wallet.balance.toLocaleString()}` : ''}
                  </span>
                ) : null}
                trailingTools={isGenerating ? (
                  <Button type="button" size="sm" variant="ghost" aria-label="取消生成" onClick={handleCancel}>
                    <XIcon data-icon="inline-start" />
                    取消
                  </Button>
                ) : hasInsufficientCredits ? (
                  <Button type="button" size="sm" variant="ghost" onClick={() => navigate('/billing')}>
                    <WalletCardsIcon data-icon="inline-start" />
                    去充值
                  </Button>
                ) : null}
              />
            </div>
          </div>
        </div>
      </div>

      <HistoryDrawer
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        onSelect={(generation) => {
          setSelectedGenerationId(generation.id)
          setCurrentGeneration(generation)
          setCurrentImages(resultsToImages(generation.results))
        }}
        onRegenerate={(generation) => {
          setPromptValue((current) => ({ ...current, prompt: generation.prompt }))
        }}
        selectedId={selectedGenerationId}
        projectId={projectID}
      />

      {previewImage && currentGeneration && currentImages.length > 0 ? (
        <ImagePreview
          images={currentImages}
          initialIndex={Math.max(0, currentImages.findIndex((image) => image.url === previewImage))}
          metadata={{
            provider: currentGeneration.provider,
            model: currentGeneration.model,
            prompt: currentGeneration.prompt,
            revisedPrompt: currentGeneration.revised_prompt,
            quality: currentGeneration.quality,
            size: currentGeneration.size,
            outputFormat: currentGeneration.output_format,
            estimatedCost: currentGeneration.estimated_cost,
            finalCost: currentGeneration.final_cost,
            billingStatus: currentGeneration.billing_status,
            createdAt: currentGeneration.created_at,
          }}
          canInpaint={effectiveCaps?.supportsMask ?? false}
          onEdit={(image) => {
            setPreviewImage(null)
            setEditingImage(image)
          }}
          onClose={() => setPreviewImage(null)}
        />
      ) : null}
    </div>
  )
}
