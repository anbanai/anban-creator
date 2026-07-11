import { useState, useCallback, useRef, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import DesignerToolbar, {
  type DesignerSettingsPatch,
} from '@/components/designer/DesignerToolbar'
import DesignerDropOverlay from '@/components/designer/DesignerDropOverlay'
import {
  admitReferenceFiles,
  isReferenceImageFile,
  isSupportedReferenceImageMimeType,
  type ReferenceAdmissionResult,
} from '@/components/designer/reference-files'
import DesignerCanvas from '@/components/designer/DesignerCanvas'
import DesignerPromptBar from '@/components/designer/DesignerPromptBar'
import InlineMaskEditor from '@/components/designer/InlineMaskEditor'
import HistoryDrawer from '@/components/designer/HistoryDrawer'
import ImagePreview from '@/components/designer/ImagePreview'
import type { DesignerSettings, DesignerProvider } from '@/types/designer'
import type { InlineMaskEditorHandle } from '@/components/designer/InlineMaskEditor'
import { designerApi } from '@/lib/api/designer'
import { getApiErrorMessage, sanitizeUserFacingErrorMessage } from '@/lib/http-client'
import type { GenerateImage, ImageGeneration, ImageGenerationResult } from '@/types/designer'
import { saveActiveGeneration, loadActiveGeneration, clearActiveGeneration } from '@/lib/designer-session'
import { buildDesignerRequestSize } from '@/lib/designer-size'

const DEFAULT_SETTINGS: DesignerSettings = {
  quality: 'auto',
  size: 'auto',
  resolution: '2K',
  n: 1,
  outputFormat: 'png',
  compression: 100,
  background: 'auto',
  referenceFiles: [],
  watermark: false,
}

const POLL_INTERVAL = 2000
const MAX_POLLS = 180 // 6 minutes at 2s interval (backend default is 5 min)
const MAX_CONSECUTIVE_ERRORS = 5

function hasExternalFiles(dataTransfer: DataTransfer): boolean {
  return Array.from(dataTransfer.types).includes('Files')
}

function hasPotentialReferenceImages(dataTransfer: DataTransfer): boolean {
  const files = Array.from(dataTransfer.files)
  if (files.length > 0) return files.some(isReferenceImageFile)

  const fileItems = Array.from(dataTransfer.items).filter(
    (item) => item.kind === 'file',
  )
  if (fileItems.length === 0) return true

  return fileItems.some(
    (item) => item.type === '' || isSupportedReferenceImageMimeType(item.type),
  )
}

function hasReferenceFileDrag(dataTransfer: DataTransfer): boolean {
  return hasExternalFiles(dataTransfer) && hasPotentialReferenceImages(dataTransfer)
}

function maxReferenceImagesForProvider(provider?: DesignerProvider): number {
  const capabilities = provider?.capabilities
  return capabilities?.supportsReference
    ? Math.max(0, capabilities.maxReferenceImages)
    : 0
}

function countIncomingReferenceImages(dataTransfer: DataTransfer): number | undefined {
  const files = Array.from(dataTransfer.files)
  if (files.length > 0) return files.filter(isReferenceImageFile).length

  const imageItems = Array.from(dataTransfer.items).filter(
    (item) => item.kind === 'file' && isSupportedReferenceImageMimeType(item.type),
  )
  return imageItems.length > 0 ? imageItems.length : undefined
}

function describeReferenceAdmission(
  result: ReferenceAdmissionResult,
  maxFiles: number,
): string | undefined {
  if (
    result.accepted === 0
    && result.rejectedOverflow > 0
    && result.rejectedNonImages === 0
    && result.rejectedDuplicates === 0
  ) {
    return `参考图已达到当前模型的 ${maxFiles} 张上限`
  }

  if (
    result.accepted === 0
    && result.rejectedDuplicates > 0
    && result.rejectedNonImages === 0
    && result.rejectedOverflow === 0
  ) {
    return '这些图片已经在参考素材中'
  }

  const details: string[] = []
  if (result.rejectedNonImages > 0) {
    details.push(`忽略 ${result.rejectedNonImages} 个非图片文件`)
  }
  if (result.rejectedDuplicates > 0) {
    details.push(`忽略 ${result.rejectedDuplicates} 张重复图片`)
  }
  if (result.rejectedOverflow > 0) {
    details.push(`另外 ${result.rejectedOverflow} 张超过当前模型的 ${maxFiles} 张上限`)
  }

  if (details.length === 0) return undefined
  if (result.accepted > 0) return `已添加 ${result.accepted} 张，${details.join('，')}`
  return details.join('，')
}

function resultsToImages(results?: ImageGenerationResult[]): GenerateImage[] {
  if (!results || results.length === 0) return []
  return results
    .filter((r) => r.image_url || r.image_path)
    .map((r, i) => ({
      url: r.image_url || r.image_path!,
      width: r.width,
      height: r.height,
      index: i,
    }))
}

export default function DesignerPage() {
  const queryClient = useQueryClient()
  const [selectedProviderId, setSelectedProviderId] = useState<string>('')
  const [settings, setSettings] = useState<DesignerSettings>(DEFAULT_SETTINGS)
  const [currentImages, setCurrentImages] = useState<GenerateImage[]>([])
  const [currentGeneration, setCurrentGeneration] = useState<ImageGeneration | null>(null)
  const [selectedGenerationId, setSelectedGenerationId] = useState<string>()
  const [previewImage, setPreviewImage] = useState<string | null>(null)
  const [prefillPrompt, setPrefillPrompt] = useState<string>('')
  const [prefillKey, setPrefillKey] = useState<string>('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [editingImage, setEditingImage] = useState<GenerateImage | null>(null)
  const maskEditorRef = useRef<InlineMaskEditorHandle>(null)
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const abortedRef = useRef(false)
  const referenceFilesRef = useRef<File[]>([])
  const referenceDragDepthRef = useRef(0)
  const normalizedProviderIdRef = useRef('')
  const normalizedReferenceCapacityRef = useRef(0)
  const [referenceDropActive, setReferenceDropActive] = useState(false)
  const [incomingReferenceCount, setIncomingReferenceCount] = useState<number>()

  const resetReferenceDrag = useCallback(() => {
    referenceDragDepthRef.current = 0
    setReferenceDropActive(false)
    setIncomingReferenceCount(undefined)
  }, [])

  const updateSettings = useCallback((patch: DesignerSettingsPatch) => {
    setSettings((current) => ({ ...current, ...patch }))
  }, [])

  const normalizeProviderState = useCallback((
    provider: DesignerProvider | undefined,
    resetSettings: boolean,
  ) => {
    const maxFiles = maxReferenceImagesForProvider(provider)
    const retainedReferenceFiles = referenceFilesRef.current.slice(0, maxFiles)
    const removedCount = referenceFilesRef.current.length - retainedReferenceFiles.length

    resetReferenceDrag()
    referenceFilesRef.current = retainedReferenceFiles

    if (resetSettings) {
      const capabilities = provider?.capabilities
      setSettings({
        ...DEFAULT_SETTINGS,
        size: capabilities?.defaultSize || DEFAULT_SETTINGS.size,
        quality: capabilities?.qualityLevels?.[0] ?? DEFAULT_SETTINGS.quality,
        n: Math.min(DEFAULT_SETTINGS.n, Math.max(1, capabilities?.maxBatch ?? 1)),
        referenceFiles: retainedReferenceFiles,
      })
    } else {
      setSettings((current) => ({
        ...current,
        referenceFiles: retainedReferenceFiles,
      }))
    }

    if (removedCount > 0) {
      toast.warning(`当前模型最多支持 ${maxFiles} 张参考图，已移除 ${removedCount} 张`)
    }
  }, [resetReferenceDrag])

  // Stop polling on unmount
  useEffect(() => {
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current)
    }
  }, [])

  // Fetch available providers from backend
  const { data: providers } = useQuery({
    queryKey: ['designer', 'providers'],
    queryFn: () => designerApi.getProviders(),
  })

  const providerList: DesignerProvider[] = providers ?? []

  // Auto-select first enabled provider if none selected or current selection is unavailable/disabled
  const activeProvider = providerList.find((p) => p.id === selectedProviderId && p.enabled)
  const effectiveProvider = activeProvider ?? providerList.find((p) => p.enabled)
  const effectiveCaps = effectiveProvider?.capabilities
  const canInpaint = effectiveCaps?.supportsMask ?? false
  const maxReferenceImages = maxReferenceImagesForProvider(effectiveProvider)
  const remainingReferenceCapacity = Math.max(
    0,
    maxReferenceImages - settings.referenceFiles.length,
  )
  const canAcceptReferenceDrop = maxReferenceImages > 0 && remainingReferenceCapacity > 0

  useEffect(() => {
    if (providers === undefined) return

    const effectiveProviderId = effectiveProvider?.id ?? ''
    if (selectedProviderId !== effectiveProviderId) {
      setSelectedProviderId(effectiveProviderId)
    }

    const providerChanged = normalizedProviderIdRef.current !== effectiveProviderId
    const capacityChanged = normalizedReferenceCapacityRef.current !== maxReferenceImages
    if (!providerChanged && !capacityChanged) return

    normalizedProviderIdRef.current = effectiveProviderId
    normalizedReferenceCapacityRef.current = maxReferenceImages
    normalizeProviderState(effectiveProvider, providerChanged)
  }, [
    effectiveProvider,
    maxReferenceImages,
    normalizeProviderState,
    providers,
    selectedProviderId,
  ])

  function stopPolling() {
    if (pollingRef.current) {
      clearInterval(pollingRef.current)
      pollingRef.current = null
    }
  }

  function startPolling(generationId: string) {
    stopPolling()
    abortedRef.current = false
    let pollCount = 0
    let consecutiveErrors = 0

    pollingRef.current = setInterval(async () => {
      if (abortedRef.current) {
        stopPolling()
        setIsGenerating(false)
        clearActiveGeneration()
        return
      }

      pollCount++
      if (pollCount >= MAX_POLLS) {
        stopPolling()
        setIsGenerating(false)
        clearActiveGeneration()
        toast.error('生成超时，请稍后在历史记录中查看结果')
        return
      }

      try {
        const gen = await designerApi.getGeneration(generationId)
        consecutiveErrors = 0

        if (gen.status === 'completed') {
          stopPolling()
          setIsGenerating(false)
          setCurrentGeneration(gen)
          setCurrentImages(resultsToImages(gen.results))
          clearActiveGeneration()
          toast.success('图片生成成功')
          queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
        } else if (gen.status === 'failed') {
          stopPolling()
          setIsGenerating(false)
          toast.error(sanitizeUserFacingErrorMessage(gen.error, '图片生成失败，请稍后重试'))
          clearActiveGeneration()
        }
      } catch {
        consecutiveErrors++
        if (consecutiveErrors >= MAX_CONSECUTIVE_ERRORS) {
          stopPolling()
          setIsGenerating(false)
          clearActiveGeneration()
          toast.error('查询生成状态失败')
        }
      }
    }, POLL_INTERVAL)
  }

  // Resume polling for active generation on mount
  useEffect(() => {
    const active = loadActiveGeneration()
    if (!active) return

    const { generationId } = active

    designerApi.getGeneration(generationId).then((gen) => {
      if (gen.status === 'completed') {
        setCurrentGeneration(gen)
        setCurrentImages(resultsToImages(gen.results))
        setSelectedGenerationId(generationId)
        clearActiveGeneration()
      } else if (gen.status === 'failed') {
        toast.error(sanitizeUserFacingErrorMessage(gen.error, '图片生成失败，请稍后重试'))
        clearActiveGeneration()
      } else {
        setSelectedGenerationId(generationId)
        setIsGenerating(true)
        startPolling(generationId)
      }
    }).catch(() => {
      clearActiveGeneration()
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleGenerate = useCallback(async (prompt: string) => {
    if (!effectiveProvider) {
      toast.error('没有可用的图片模型')
      return
    }

    stopPolling()
    abortedRef.current = false
    setIsGenerating(true)
    setCurrentGeneration(null)
    setCurrentImages([])

    try {
      // Upload only the ordered references currently supported by the effective provider.
      const referenceFilesForGeneration = maxReferenceImages > 0
        ? referenceFilesRef.current.slice(0, maxReferenceImages)
        : []
      const refFileIds = referenceFilesForGeneration.length > 0
        ? (await Promise.all(
            referenceFilesForGeneration.map((file) => designerApi.uploadReference(file)),
          )).map((r) => r.file_id)
        : []

      // Start async generation
      const requestSize = buildDesignerRequestSize(settings.size, settings.resolution)
      const { generation_id } = await designerApi.generate({
        project_id: '',
        prompt,
        provider: effectiveProvider.provider,
        provider_id: effectiveProvider.id,
        quality: settings.quality !== 'auto' ? settings.quality : undefined,
        size: requestSize,
        n: settings.n > 1 ? settings.n : undefined,
        output_format: settings.outputFormat !== 'png' ? settings.outputFormat : undefined,
        output_compression: effectiveCaps?.hasCompression && settings.compression < 100 ? settings.compression : undefined,
        background: effectiveCaps?.hasBackground && settings.background !== 'auto' ? settings.background : undefined,
        reference_file_ids: refFileIds.length > 0 ? refFileIds : undefined,
        watermark: settings.watermark || undefined,
      })

      setSelectedGenerationId(generation_id)
      saveActiveGeneration(generation_id)
      startPolling(generation_id)
    } catch (err) {
      setIsGenerating(false)
      toast.error(getApiErrorMessage(err, '图片生成失败，请重试'))
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveProvider, maxReferenceImages, settings, queryClient])

  const handleHistorySelect = useCallback((gen: ImageGeneration) => {
    setSelectedGenerationId(gen.id)
    setCurrentGeneration(gen)
    setCurrentImages(resultsToImages(gen.results))
  }, [])

  const handleRegenerate = useCallback((gen: ImageGeneration) => {
    setPrefillPrompt(gen.prompt)
    setPrefillKey(gen.id)
  }, [])

  const addReferenceFiles = useCallback(
    (incomingFiles: File[]) => {
      const result = admitReferenceFiles(
        referenceFilesRef.current,
        incomingFiles,
        maxReferenceImages,
      )

      referenceFilesRef.current = result.files
      setSettings((current) => ({
        ...current,
        referenceFiles: result.files,
      }))

      const message = describeReferenceAdmission(result, maxReferenceImages)
      if (message) toast.warning(message)
    },
    [maxReferenceImages],
  )

  const removeReferenceFile = useCallback((index: number) => {
    const files = referenceFilesRef.current.filter(
      (_, fileIndex) => fileIndex !== index,
    )
    referenceFilesRef.current = files
    setSettings((current) => ({ ...current, referenceFiles: files }))
  }, [])

  useEffect(() => {
    if (!referenceDropActive) return

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') resetReferenceDrag()
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [referenceDropActive, resetReferenceDrag])

  function handleReferenceDragEnter(event: React.DragEvent<HTMLDivElement>) {
    if (!canAcceptReferenceDrop || !hasReferenceFileDrag(event.dataTransfer)) return
    event.preventDefault()
    referenceDragDepthRef.current += 1
    setReferenceDropActive(true)
    setIncomingReferenceCount(countIncomingReferenceImages(event.dataTransfer))
  }

  function handleReferenceDragOver(event: React.DragEvent<HTMLDivElement>) {
    if (maxReferenceImages === 0 || !hasReferenceFileDrag(event.dataTransfer)) return
    event.preventDefault()
    event.dataTransfer.dropEffect = 'copy'
  }

  function handleReferenceDragLeave(event: React.DragEvent<HTMLDivElement>) {
    if (!hasExternalFiles(event.dataTransfer)) return
    event.preventDefault()
    referenceDragDepthRef.current = Math.max(0, referenceDragDepthRef.current - 1)
    if (referenceDragDepthRef.current === 0) resetReferenceDrag()
  }

  function handleReferenceDrop(event: React.DragEvent<HTMLDivElement>) {
    if (!hasExternalFiles(event.dataTransfer)) return
    event.preventDefault()
    const files = Array.from(event.dataTransfer.files)
    resetReferenceDrag()
    if (maxReferenceImages === 0) return
    if (files.length > 0) addReferenceFiles(files)
  }

  function handleModelChange(providerId: string) {
    const newProvider = providerList.find((provider) => provider.id === providerId)
    const maxFiles = maxReferenceImagesForProvider(newProvider)

    setSelectedProviderId(providerId)
    normalizedProviderIdRef.current = providerId
    normalizedReferenceCapacityRef.current = maxFiles
    normalizeProviderState(newProvider, true)
  }

  function handleCancel() {
    if (editingImage) {
      setEditingImage(null)
      return
    }
    abortedRef.current = true
    stopPolling()
    setIsGenerating(false)
    clearActiveGeneration()
  }

  const handleEditSubmit = useCallback(async (prompt: string) => {
    if (!effectiveProvider || !editingImage) return

    const maskFile = await maskEditorRef.current?.exportMask()
    if (!maskFile) {
      toast.error('请先涂抹需要编辑的区域')
      return
    }

    stopPolling()
    abortedRef.current = false
    setIsGenerating(true)
    setEditingImage(null)

    try {
      const [sourceRes, maskRes] = await Promise.all([
        designerApi.uploadReferenceFromUrl(editingImage.url),
        designerApi.uploadReference(maskFile),
      ])

      const { generation_id } = await designerApi.generate({
        project_id: '',
        prompt: prompt.trim(),
        provider: effectiveProvider.provider,
        provider_id: effectiveProvider.id,
        reference_file_ids: [sourceRes.file_id],
        mask_file_id: maskRes.file_id,
      })

      setSelectedGenerationId(generation_id)
      saveActiveGeneration(generation_id)
      startPolling(generation_id)
    } catch (err) {
      setIsGenerating(false)
      toast.error(getApiErrorMessage(err, '编辑图片失败，请重试'))
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveProvider, editingImage])

  return (
    // Full-bleed: negate AppLayout padding
    <div
      data-testid="designer-workspace"
      className="relative -mx-4 -my-6 flex overflow-hidden bg-background md:-mx-8 md:-my-8"
      style={{ height: '100dvh' }}
      onDragEnter={handleReferenceDragEnter}
      onDragOver={handleReferenceDragOver}
      onDragLeave={handleReferenceDragLeave}
      onDrop={handleReferenceDrop}
    >
      {/* Sidebar: full-height floating panel */}
      <DesignerToolbar
        providers={providerList}
        selectedProviderId={effectiveProvider?.id ?? ''}
        onModelChange={handleModelChange}
        capabilities={effectiveProvider?.capabilities}
        settings={settings}
        onSettingsChange={updateSettings}
        onHistoryToggle={() => setHistoryOpen(true)}
        onReferenceFilesAdded={addReferenceFiles}
        onReferenceFileRemove={removeReferenceFile}
        referenceDropActive={referenceDropActive}
      />

      {/* Main area: canvas workspace */}
      <div className="relative flex min-h-0 flex-1 flex-col p-3 pl-0">
        <div
          data-testid="designer-canvas-frame"
          className="relative flex-1 overflow-hidden rounded-2xl border border-border/70 bg-card/35 shadow-inner"
          style={editingImage ? undefined : { backgroundImage: 'radial-gradient(circle, color-mix(in oklch, var(--color-border) 55%, transparent) 0.5px, transparent 0.5px)', backgroundSize: '20px 20px' }}
        >
          <div className="h-full overflow-y-auto p-4 pb-24 md:p-6 md:pb-28">
            {editingImage ? (
              <InlineMaskEditor
                ref={maskEditorRef}
                imageUrl={editingImage.url}
                onClose={() => setEditingImage(null)}
              />
            ) : (
              <DesignerCanvas
                images={currentImages}
                isGenerating={isGenerating}
                canInpaint={canInpaint}
                onImageClick={(img) => setPreviewImage(img.url)}
                onEdit={(img) => setEditingImage(img)}
              />
            )}
          </div>
          <DesignerPromptBar
            onSubmit={editingImage ? handleEditSubmit : handleGenerate}
            isGenerating={isGenerating}
            onCancel={handleCancel}
            initialPrompt={prefillPrompt}
            initialPromptKey={prefillKey}
            onInitialPromptConsumed={() => { setPrefillPrompt(''); setPrefillKey('') }}
            editMode={!!editingImage}
          />
        </div>
      </div>

      <DesignerDropOverlay
        active={referenceDropActive}
        incomingCount={incomingReferenceCount}
        remainingCapacity={remainingReferenceCapacity}
      />

      {/* History drawer */}
      <HistoryDrawer
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        onSelect={handleHistorySelect}
        onRegenerate={handleRegenerate}
        selectedId={selectedGenerationId}
      />

      {/* Image preview modal */}
      {previewImage && currentGeneration && currentImages.length > 0 && (
        <ImagePreview
          images={currentImages}
          initialIndex={Math.max(0, currentImages.findIndex((i) => i.url === previewImage))}
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
            totalTokens: currentGeneration.total_tokens,
            createdAt: currentGeneration.created_at,
          }}
          canInpaint={canInpaint}
          onEdit={(img) => {
            setPreviewImage(null)
            setEditingImage(img)
          }}
          onClose={() => setPreviewImage(null)}
        />
      )}
    </div>
  )
}
