import { isAxiosError } from 'axios'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { PaintbrushIcon, SendIcon, WalletCardsIcon, XIcon } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import DesignerToolbar, { type DesignerSettingsPatch } from '@/components/designer/DesignerToolbar'
import { DesignerGenerationToolbar } from '@/components/designer/DesignerGenerationToolbar'
import DesignerCanvas from '@/components/designer/DesignerCanvas'
import InlineMaskEditor from '@/components/designer/InlineMaskEditor'
import HistoryDrawer from '@/components/designer/HistoryDrawer'
import ImagePreview from '@/components/designer/ImagePreview'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { designerApi } from '@/lib/api/designer'
import { uploadToOSS } from '@/lib/direct-upload'
import { getApiErrorCode, getApiErrorMessage, sanitizeUserFacingErrorMessage } from '@/lib/http-client'
import { clearActiveGeneration, loadActiveGeneration, saveActiveGeneration } from '@/lib/designer-session'
import {
  AttachmentRejectionReason,
  type AgentPromptValue,
  type AttachmentAdmissionPolicy,
  type AttachmentRejection,
} from '@/types/input-attachment'
import type { DesignerCapability, DesignerSettings, GenerateRequest, GenerateImage, ImageGeneration, ImageGenerationResult } from '@/types/designer'
import type { InlineMaskEditorHandle } from '@/components/designer/InlineMaskEditor'

const DEFAULT_SETTINGS: DesignerSettings = {
  quality: 'auto',
  size: '',
  n: 1,
  outputFormat: 'png',
  compression: 100,
  background: 'auto',
  watermark: false,
}

const EMPTY_PROMPT_VALUE: AgentPromptValue = { prompt: '', attachments: [] }
const MAX_DESIGNER_REFERENCE_BYTES = 10 * 1024 * 1024
const POLL_INTERVAL = 2000
const MAX_CONSECUTIVE_ERRORS = 5

function maxReferenceImagesForCapability(capability?: DesignerCapability): number {
  const designerFeatures = capability?.designerFeatures
  return designerFeatures?.supportsReference
    ? Math.max(0, designerFeatures.maxReferenceImages)
    : 0
}

function resultsToImages(results?: ImageGenerationResult[]): GenerateImage[] {
  if (!results?.length) return []
  return results
    .filter((result) => result.image_url)
    .map((result, index) => ({
      url: result.image_url!,
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
  const { user } = useAuth()
  const userId = user?.id ?? ''
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [selectedCapabilityKey, setSelectedCapabilityKey] = useState('')
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
  const [retainedInputs, setRetainedInputs] = useState<{ references: string[]; mask?: string } | null>(null)
  const [stoppedWaiting, setStoppedWaiting] = useState(false)
  const pendingRequestRef = useRef<GenerateRequest | null>(null)
  const registeredReferencesRef = useRef(new Map<string, string>())
  const [retryAvailable, setRetryAvailable] = useState(false)
  const [editingImage, setEditingImage] = useState<GenerateImage | null>(null)
  const maskEditorRef = useRef<InlineMaskEditorHandle>(null)
  const pollingRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortedRef = useRef(false)
  const generationAttemptRef = useRef(0)
  const generationAbortRef = useRef<AbortController | null>(null)
  const normalizedCapabilityKeyRef = useRef('')
  const normalizedReferenceCapacityRef = useRef(0)

  const setPromptValue = useCallback((next: AgentPromptValue | ((current: AgentPromptValue) => AgentPromptValue)) => {
    setPromptValueState((current) => {
      const resolved = typeof next === 'function' ? next(current) : next
      promptValueRef.current = resolved
      return resolved
    })
  }, [])

  const { data: imageCapabilityCatalog } = useQuery({
    queryKey: ['designer', 'capabilities'],
    queryFn: () => designerApi.getCapabilities(),
  })
  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ['projects', 'designer', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
  })
  const { data: wallet, isError: walletError } = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })

  const capabilityList: DesignerCapability[] = imageCapabilityCatalog?.items ?? []
  const activeCapability = capabilityList.find((capability) => capability.id === selectedCapabilityKey && capability.enabled && capability.priceAvailable === true)
  const configuredDefaultCapability = capabilityList.find((capability) => capability.id === imageCapabilityCatalog?.defaultCapability && capability.enabled && capability.priceAvailable === true)
  const effectiveCapability = activeCapability ?? configuredDefaultCapability ?? capabilityList.find((capability) => capability.enabled && capability.priceAvailable === true)
  const hasInsufficientCredits = Boolean(
    !walletError && wallet && effectiveCapability && wallet.balance < effectiveCapability.credits,
  )
  const effectiveDesignerFeatures = effectiveCapability?.designerFeatures
  const effectiveMaxBatch = 1
  const effectiveQuantity = Math.min(effectiveMaxBatch, Math.max(1, settings.n))
  const effectiveSettings = settings.n === effectiveQuantity
    ? settings
    : { ...settings, n: effectiveQuantity }
  const maxReferenceImages = maxReferenceImagesForCapability(effectiveCapability)
  const referenceCapacity = maxReferenceImages
  const projectID = selectedProjectId ?? ''
  const attachmentPolicy = useMemo<AttachmentAdmissionPolicy>(() => ({
    allowedTypes: ['image'],
    maxCount: editingImage ? 0 : referenceCapacity,
    maxBytes: { image: MAX_DESIGNER_REFERENCE_BYTES },
  }), [editingImage, referenceCapacity])

  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'designer_reference' },
    policy: attachmentPolicy,
    attachments: promptValue.attachments,
    onAttachmentsChange: (attachments) => {
      setRetainedInputs(null)
      setPromptValue((current) => ({ ...current, attachments }))
    },
  })

  const updateSettings = useCallback((patch: DesignerSettingsPatch) => {
    setSettings((current) => ({ ...current, ...patch }))
  }, [])

  const resetSettingsForCapability = useCallback((capability?: DesignerCapability) => {
    const designerFeatures = capability?.designerFeatures
    setSettings({
      ...DEFAULT_SETTINGS,
      size: designerFeatures?.defaultSize || DEFAULT_SETTINGS.size,
      quality: designerFeatures?.qualityLevels?.[0] ?? DEFAULT_SETTINGS.quality,
      outputFormat: designerFeatures?.outputFormats?.[0] ?? DEFAULT_SETTINGS.outputFormat,
      n: Math.min(DEFAULT_SETTINGS.n, Math.max(1, designerFeatures?.maxBatch ?? 1)),
    })
  }, [])

  const startGenerationAttempt = useCallback(() => {
    generationAbortRef.current?.abort()
    const controller = new AbortController()
    generationAbortRef.current = controller
    generationAttemptRef.current += 1
    abortedRef.current = false
    setStoppedWaiting(false)
    setRetryAvailable(false)
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
    if (imageCapabilityCatalog === undefined) return
    const effectiveCapabilityKey = effectiveCapability?.id ?? ''
    if (selectedCapabilityKey !== effectiveCapabilityKey) setSelectedCapabilityKey(effectiveCapabilityKey)

    const capabilityChanged = normalizedCapabilityKeyRef.current !== effectiveCapabilityKey
    const capacityChanged = normalizedReferenceCapacityRef.current !== referenceCapacity
    if (!capabilityChanged && !capacityChanged) return
    normalizedCapabilityKeyRef.current = effectiveCapabilityKey
    normalizedReferenceCapacityRef.current = referenceCapacity
    if (capabilityChanged) resetSettingsForCapability(effectiveCapability)

    const overflow = promptValueRef.current.attachments.slice(referenceCapacity)
    for (const attachment of overflow) attachmentController.remove(attachment.id)
    if (overflow.length > 0) {
      toast.warning(`当前输入最多支持 ${referenceCapacity} 张参考图，已移除 ${overflow.length} 张`)
    }
  }, [attachmentController, effectiveCapability, imageCapabilityCatalog, referenceCapacity, resetSettingsForCapability, selectedCapabilityKey])

  useEffect(() => {
    setSettings((current) => {
      const n = Math.min(effectiveMaxBatch, Math.max(1, current.n))
      return current.n === n ? current : { ...current, n }
    })
  }, [effectiveMaxBatch])

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
    let consecutiveErrors = 0

    const scheduleNext = () => {
      if (!isGenerationAttemptActive(attempt)) return
      pollingRef.current = setTimeout(() => { void poll() }, POLL_INTERVAL)
    }

    const poll = async () => {
      if (!isGenerationAttemptActive(attempt)) return
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
          clearActiveGeneration(userId)
          void queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] })
          toast.success('图片生成成功')
          void queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
        } else if (generation.status === 'failed') {
          if (!finalizeGenerationAttempt(attempt)) return
          setIsGenerating(false)
          clearActiveGeneration(userId)
          void queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] })
          void queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
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
          setStoppedWaiting(true)
          toast.error('查询生成状态失败，可刷新或在历史记录中查看结果')
        } else {
          scheduleNext()
        }
      }
    }

    scheduleNext()
  }, [finalizeGenerationAttempt, isGenerationAttemptActive, queryClient, stopPolling, userId])

  useEffect(() => {
    generationAttemptRef.current += 1
    generationAbortRef.current?.abort()
    stopPolling()
    setPromptValue(EMPTY_PROMPT_VALUE)
    setEditingImage(null)
    setPreviewImage(null)
    setSelectedGenerationId(undefined)
    setStoppedWaiting(false)
    setCurrentGeneration(null)
    setCurrentImages([])
    setRetainedInputs(null)
    pendingRequestRef.current = null
    registeredReferencesRef.current.clear()
    setRetryAvailable(false)
    setIsGenerating(false)
    const active = loadActiveGeneration(userId)
    if (!active) return
    const attempt = startGenerationAttempt()
    awaitGenerationAttempt(
      designerApi.getGeneration(active.generationId, attempt.signal),
      attempt.signal,
    ).then((generation) => {
      if (!isGenerationAttemptActive(attempt)) return
      if (generation.status === 'completed') {
        void queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] })
        void queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
        if (!finalizeGenerationAttempt(attempt)) return
        setCurrentGeneration(generation)
        setCurrentImages(resultsToImages(generation.results))
        setSelectedGenerationId(active.generationId)
        clearActiveGeneration(userId)
      } else if (generation.status === 'failed') {
        void queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] })
        void queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
        if (!finalizeGenerationAttempt(attempt)) return
        toast.error(sanitizeUserFacingErrorMessage(generation.error, '图片生成失败，请稍后重试'))
        clearActiveGeneration(userId)
      } else {
        setSelectedGenerationId(active.generationId)
        setIsGenerating(true)
        startPolling(active.generationId, attempt)
      }
    }).catch(() => {
      if (isGenerationAttemptActive(attempt)) {
        setIsGenerating(false)
        setStoppedWaiting(true)
      }
    })
    return () => {
      generationAttemptRef.current += 1
      generationAbortRef.current?.abort()
      stopPolling()
    }
  }, [finalizeGenerationAttempt, isGenerationAttemptActive, startGenerationAttempt, startPolling, stopPolling, setPromptValue, queryClient, userId])

  const refreshWallet = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] })
  }, [queryClient])

  const beginGeneration = useCallback((generationID: string, attempt: GenerationAttempt) => {
    if (!isGenerationAttemptActive(attempt)) return
    refreshWallet()
    setSelectedGenerationId(generationID)
    saveActiveGeneration(userId, generationID)
    startPolling(generationID, attempt)
  }, [isGenerationAttemptActive, refreshWallet, startPolling, userId])

  const showGenerationError = useCallback((error: unknown) => {
    const message = getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')
    if (getApiErrorCode(error) === 40203) {
      refreshWallet()
      toast.error(message, {
        action: { label: '去充值', onClick: () => navigate('/billing') },
      })
      return
    }
    toast.error(message)
  }, [navigate, refreshWallet])

  const dispatchGeneration = useCallback(async (request: GenerateRequest, attempt: GenerationAttempt) => {
    const previous = pendingRequestRef.current
    const fingerprint = (value: GenerateRequest) => JSON.stringify({ ...value, operation_id: undefined })
    const operationId = previous && fingerprint(previous) === fingerprint(request)
      ? previous.operation_id
      : crypto.randomUUID()
    const pending = { ...request, operation_id: operationId }
    pendingRequestRef.current = pending
    setRetryAvailable(false)
    let admissionStarted = false
    try {
      const result = await designerApi.generate(pending, attempt.signal, (id) => {
        attempt.signal.throwIfAborted()
        admissionStarted = true
        saveActiveGeneration(userId, id)
      })
      // Even a late accepted response must remain recoverable after stopping local waiting.
      if (isGenerationAttemptActive(attempt) || !loadActiveGeneration(userId)) {
        saveActiveGeneration(userId, result.generation_id)
      }
      if (isGenerationAttemptActive(attempt)) {
        pendingRequestRef.current = null
        setRetryAvailable(false)
      }
      return result
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      const rejected = admissionStarted && status !== undefined && status >= 400 && status < 500 && status !== 408
      if (rejected && loadActiveGeneration(userId)?.generationId === operationId) {
        clearActiveGeneration(userId)
      }
      if (isGenerationAttemptActive(attempt)) {
        if (rejected) pendingRequestRef.current = null
        setRetryAvailable(!rejected)
      }
      throw error
    }
  }, [isGenerationAttemptActive, userId])

  const handleGenerate = useCallback(async (value: AgentPromptValue) => {
    if (!effectiveCapability) {
      toast.error('没有可用的图像能力')
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
      const referenceFileIDs: string[] = retainedInputs?.references.slice(0, referenceCapacity) ?? []
      for (const attachment of supportedReferences) {
        if (!attachment.uploadId || !attachment.key || attachment.status !== 'uploaded') {
          throw new Error('参考图仍在上传或上传失败')
        }
        const cached = registeredReferencesRef.current.get(attachment.uploadId)
        if (cached) { referenceFileIDs.push(cached); continue }
        const registered = await awaitGenerationAttempt(
          designerApi.registerReference({
            upload_id: attachment.uploadId,
            key: attachment.key,
          }, attempt.signal),
          attempt.signal,
        )
        if (!isGenerationAttemptActive(attempt)) return
        registeredReferencesRef.current.set(attachment.uploadId, registered.file_id)
        referenceFileIDs.push(registered.file_id)
      }
      const { generation_id } = await awaitGenerationAttempt(
        dispatchGeneration({
          project_id: projectID,
          prompt: value.prompt.trim(),
          capability_key: effectiveCapability.id,
          quality: settings.quality,
          size: settings.size,
          n: effectiveQuantity,
          output_format: settings.outputFormat,
          output_compression: effectiveDesignerFeatures?.hasCompression && settings.outputFormat !== 'png' && settings.compression < 100 ? settings.compression : undefined,
          background: effectiveDesignerFeatures?.hasBackground && settings.background !== 'auto' ? settings.background : undefined,
          reference_file_ids: referenceFileIDs.length > 0 ? referenceFileIDs : undefined,
          mask_file_id: effectiveDesignerFeatures?.supportsMask ? retainedInputs?.mask : undefined,
          watermark: effectiveDesignerFeatures?.watermark ? settings.watermark : undefined,
        }, attempt),
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
  }, [dispatchGeneration, retainedInputs, beginGeneration, clearSubmittedPrompt, effectiveDesignerFeatures, effectiveCapability, effectiveQuantity, isGenerationAttemptActive, projectID, referenceCapacity, settings, showGenerationError, startGenerationAttempt, stopPolling])

  const handleEditSubmit = useCallback(async (value: AgentPromptValue) => {
    if (!effectiveCapability || !editingImage || !effectiveDesignerFeatures?.supportsMask) return
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
        dispatchGeneration({
          project_id: projectID,
          prompt: value.prompt.trim(),
          capability_key: effectiveCapability.id,
          quality: settings.quality,
          size: settings.size,
          n: effectiveQuantity,
          output_format: settings.outputFormat,
          output_compression: effectiveDesignerFeatures?.hasCompression && settings.outputFormat !== 'png' && settings.compression < 100 ? settings.compression : undefined,
          background: effectiveDesignerFeatures?.hasBackground && settings.background !== 'auto' ? settings.background : undefined,
          reference_file_ids: [source.file_id],
          mask_file_id: mask.file_id,
          watermark: effectiveDesignerFeatures?.watermark ? settings.watermark : undefined,
        }, attempt),
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
  }, [dispatchGeneration, beginGeneration, clearSubmittedPrompt, editingImage, effectiveCapability, effectiveDesignerFeatures, effectiveQuantity, isGenerationAttemptActive, projectID, settings, showGenerationError, startGenerationAttempt, stopPolling])

  const handleCancel = useCallback(() => {
    generationAttemptRef.current += 1
    abortedRef.current = true
    generationAbortRef.current?.abort()
    generationAbortRef.current = null
    stopPolling()
    setIsGenerating(false)
    setStoppedWaiting(true)
    setRetryAvailable(pendingRequestRef.current !== null)
    if (editingImage) {
      setEditingImage(null)
    }
  }, [editingImage, stopPolling])

  const handleAttachmentRejected = useCallback((rejections: AttachmentRejection[]) => {
    const message = describeAttachmentRejections(rejections)
    if (message) toast.warning(message)
  }, [])

  function handleCapabilityChange(capabilityKey: string) {
    setRetainedInputs(null)
    setEditingImage(null)
    const capability = capabilityList.find((candidate) => candidate.id === capabilityKey)
    setSelectedCapabilityKey(capabilityKey)
    normalizedCapabilityKeyRef.current = capabilityKey
    normalizedReferenceCapacityRef.current = maxReferenceImagesForCapability(capability)
    resetSettingsForCapability(capability)
    const overflow = promptValueRef.current.attachments.slice(maxReferenceImagesForCapability(capability))
    for (const attachment of overflow) attachmentController.remove(attachment.id)
    if (overflow.length > 0) {
      toast.warning(`当前能力最多支持 ${maxReferenceImagesForCapability(capability)} 张参考图，已移除 ${overflow.length} 张`)
    }
  }

  const selectedProject = projects.find((project) => project.id === selectedProjectId)
  const projectControl = (
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
      ariaLabel={selectedProject
        ? `项目：${selectedProject.name}`
        : projectsLoading
          ? '项目：加载中'
          : '项目：未选择'}
      compact
    />
  )

  return (
    <div
      data-testid="designer-workspace"
      className="relative flex h-full min-h-0 flex-col overflow-hidden bg-background md:flex-row"
    >
      <DesignerToolbar
        designerFeatures={effectiveDesignerFeatures}
        settings={settings}
        onSettingsChange={updateSettings}
        onHistoryToggle={() => setHistoryOpen(true)}
      />

      <div className="relative flex min-h-0 w-full min-w-0 flex-1 flex-col p-3 md:w-auto md:pl-0">
        <div
          data-testid="designer-canvas-frame"
          className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-2xl border border-border/70 bg-card/35 shadow-inner"
          style={editingImage ? undefined : { backgroundImage: 'radial-gradient(circle, color-mix(in oklch, var(--color-border) 55%, transparent) 0.5px, transparent 0.5px)', backgroundSize: '20px 20px' }}
        >
          <div className="min-h-0 flex-1 overflow-y-auto p-4 md:p-6">
            {editingImage ? (
              <InlineMaskEditor ref={maskEditorRef} imageUrl={editingImage.url} onClose={() => setEditingImage(null)} />
            ) : (
              <DesignerCanvas
                images={currentImages}
                isGenerating={isGenerating}
                canInpaint={effectiveDesignerFeatures?.supportsMask ?? false}
                onImageClick={(image) => setPreviewImage(image.url)}
                onEdit={(image) => { setRetainedInputs(null); attachmentController.clear(); setEditingImage(image) }}
              />
            )}
          </div>
          <div className="max-h-[55%] shrink-0 overflow-y-auto px-4 pb-4 md:px-6 md:pb-6">
            <div className="w-full">
              {retainedInputs && <div className="mb-2 flex items-center gap-2 rounded-lg bg-muted p-2 text-xs">
                <span>{`已保留历史参考图 ${retainedInputs.references.length} 张${retainedInputs.mask ? '及蒙版' : ''}`}</span>
                <Button size="sm" variant="ghost" onClick={() => setRetainedInputs(null)}>清除历史输入</Button>
              </div>}
              {stoppedWaiting && <p role="status" className="mb-2 text-xs text-muted-foreground">已停止等待；已受理的任务仍会继续处理，可在历史记录或刷新后查看结果</p>}
              {retryAvailable && !isGenerating && <Button size="sm" variant="outline" onClick={async () => {
                const request = pendingRequestRef.current
                if (!request) return
                const attempt = startGenerationAttempt()
                setIsGenerating(true)
                try {
                  const result = await dispatchGeneration(request, attempt)
                  if (isGenerationAttemptActive(attempt)) { clearSubmittedPrompt(request.prompt); beginGeneration(result.generation_id, attempt) }
                } catch (error) { if (isGenerationAttemptActive(attempt)) { setIsGenerating(false); showGenerationError(error) } }
              }}>重试上次提交</Button>}
              <AgentPromptInput
                value={promptValue}
                onChange={setPromptValue}
                onSubmit={editingImage ? handleEditSubmit : handleGenerate}
                attachmentController={attachmentController}
                attachmentPolicy={attachmentPolicy}
                leadingTools={(
                  <div className="flex min-w-0 flex-wrap items-center gap-1">
                    {projectControl}
                    {!editingImage ? (
                      effectiveCapability ? (
                        <DesignerGenerationToolbar
                          capabilities={capabilityList}
                          capabilityKey={effectiveCapability.id}
                          settings={effectiveSettings}
                          onCapabilityChange={handleCapabilityChange}
                          onSettingsChange={updateSettings}
                          disabled={isGenerating}
                        />
                      ) : <span className="px-2 text-xs text-muted-foreground">暂无可用图像能力</span>
                    ) : null}
                  </div>
                )}
                placeholder={editingImage ? '描述你想修改的区域...' : '描述你想要生成的图片...'}
                submitLabel={editingImage ? '编辑' : '生成'}
                submitIcon={editingImage ? PaintbrushIcon : SendIcon}
                submitting={isGenerating}
                submitDisabled={!promptValue.prompt.trim() || !effectiveCapability || effectiveCapability.priceAvailable === false || hasInsufficientCredits}
                ariaLabel="Designer prompt"
                acceptedTypesLabel="图片"
                onAttachmentRejected={handleAttachmentRejected}
                status={isGenerating ? (
                  <span className="text-xs text-muted-foreground">正在生成...</span>
                ) : effectiveCapability ? (
                  <span className="text-xs text-muted-foreground">
                    每张 {effectiveCapability.credits.toLocaleString()} 积分
                    {!walletError && wallet ? ` · 余额 ${wallet.balance.toLocaleString()}` : ''}
                  </span>
                ) : null}
                trailingTools={isGenerating ? (
                  <Button type="button" size="sm" variant="ghost" aria-label="停止等待" onClick={handleCancel}>
                    <XIcon data-icon="inline-start" />
                    停止等待
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
          if (isGenerating) handleCancel()
          if (generation.status === 'generating') {
            const attempt = startGenerationAttempt()
            setIsGenerating(true)
            beginGeneration(generation.id, attempt)
          }
          setSelectedGenerationId(generation.id)
          setCurrentGeneration(generation)
          setCurrentImages(resultsToImages(generation.results))
        }}
        onRegenerate={(generation) => {
          if (isGenerating) handleCancel()
          const capability = capabilityList.find((item) => item.id === generation.capability_key && item.enabled && item.priceAvailable === true)
          if (!capability) { toast.error('历史图像能力当前不可用'); return }
          handleCapabilityChange(capability.id)
          setSelectedProjectId(generation.project_id || null)
          const features = capability.designerFeatures
          setSettings({
            quality: features.qualityLevels.includes(generation.quality ?? '') ? generation.quality! : features.qualityLevels[0] ?? 'auto',
            size: features.sizePresets.includes(generation.size ?? '') ? generation.size! : features.defaultSize,
            outputFormat: features.outputFormats.includes(generation.output_format ?? '') ? generation.output_format! : features.outputFormats[0] ?? 'png',
            n: 1, compression: features.hasCompression ? generation.output_compression ?? 100 : 100,
            background: features.hasBackground ? generation.background || 'auto' : 'auto',
            watermark: features.watermark && (generation.watermark ?? false),
          })
          attachmentController.clear()
          const references = features.supportsReference ? (generation.reference_file_ids ?? []).slice(0, features.maxReferenceImages) : []
          setRetainedInputs(references.length ? { references, mask: features.supportsMask ? generation.mask_file_id : undefined } : null)
          setPromptValue({ prompt: generation.prompt, attachments: [] })
        }}
        selectedId={selectedGenerationId}
        projectId={projectID}
      />

      {previewImage && currentGeneration && currentImages.length > 0 ? (
        <ImagePreview
          images={currentImages}
          initialIndex={Math.max(0, currentImages.findIndex((image) => image.url === previewImage))}
          metadata={{
            capabilityName: currentGeneration.capability_name ?? effectiveCapability?.name,
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
          canInpaint={effectiveDesignerFeatures?.supportsMask ?? false}
          onEdit={(image) => {
            setPreviewImage(null)
            setRetainedInputs(null)
            attachmentController.clear()
            setEditingImage(image)
          }}
          onClose={() => setPreviewImage(null)}
        />
      ) : null}
    </div>
  )
}
