import { useState, useCallback, useRef, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import DesignerToolbar from '@/components/designer/DesignerToolbar'
import DesignerCanvas from '@/components/designer/DesignerCanvas'
import DesignerPromptBar from '@/components/designer/DesignerPromptBar'
import InlineMaskEditor from '@/components/designer/InlineMaskEditor'
import HistoryDrawer from '@/components/designer/HistoryDrawer'
import ImagePreview from '@/components/designer/ImagePreview'
import type { DesignerSettings, DesignerProvider } from '@/types/designer'
import type { InlineMaskEditorHandle } from '@/components/designer/InlineMaskEditor'
import { designerApi } from '@/lib/api/designer'
import { getApiErrorMessage } from '@/lib/http-client'
import type { GenerateImage, ImageGeneration, ImageGenerationResult } from '@/types/designer'
import { getModelCapabilities } from '@/types/designer'
import { saveActiveGeneration, loadActiveGeneration, clearActiveGeneration } from '@/lib/designer-session'

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
  const effectiveCaps = getModelCapabilities(effectiveProvider?.provider ?? '')
  const canInpaint = effectiveCaps?.inpainting ?? false

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
          toast.error(gen.error || '图片生成失败')
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
        toast.error(gen.error || '图片生成失败')
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
      // Upload reference files in parallel
      const refFileIds = settings.referenceFiles.length > 0
        ? (await Promise.all(
            settings.referenceFiles.map((file) => designerApi.uploadReference(file)),
          )).map((r) => r.file_id)
        : []

      // Start async generation
      const sizeWithTier = settings.resolution === '2K' ? settings.size : `${settings.size}:${settings.resolution}`
      const { generation_id } = await designerApi.generate({
        channel_id: '',
        prompt,
        provider: effectiveProvider.provider,
        provider_id: effectiveProvider.id,
        quality: settings.quality !== 'auto' ? settings.quality : undefined,
        size: sizeWithTier,
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
  }, [effectiveProvider, settings, queryClient])

  const handleHistorySelect = useCallback((gen: ImageGeneration) => {
    setSelectedGenerationId(gen.id)
    setCurrentGeneration(gen)
    setCurrentImages(resultsToImages(gen.results))
  }, [])

  const handleRegenerate = useCallback((gen: ImageGeneration) => {
    setPrefillPrompt(gen.prompt)
    setPrefillKey(gen.id)
  }, [])

  function handleModelChange(providerId: string) {
    setSelectedProviderId(providerId)
    const newProvider = providerList.find((p) => p.id === providerId)
    const caps = getModelCapabilities(newProvider?.provider ?? '')
    setSettings({
      ...DEFAULT_SETTINGS,
      ...(caps?.batch ? {} : { n: 1 }),
      ...(caps?.sizePresets?.length ? {} : { size: '1:1' }),
    })
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
        channel_id: '',
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
    <div className="-mx-4 -my-6 flex overflow-hidden bg-background md:-mx-8 md:-my-8" style={{ height: '100dvh' }}>
      {/* Sidebar: full-height floating panel */}
      <DesignerToolbar
        providers={providerList}
        selectedProviderId={effectiveProvider?.id ?? ''}
        onModelChange={handleModelChange}
        provider={effectiveProvider?.provider ?? ''}
        settings={settings}
        onSettingsChange={setSettings}
        onHistoryToggle={() => setHistoryOpen(true)}
      />

      {/* Main area: canvas + prompt */}
      <div className="relative flex min-h-0 flex-1 flex-col p-3 pl-0">
        <div
          className="relative flex-1 overflow-y-auto rounded-2xl border border-border/70 bg-card/35 p-4 shadow-inner md:p-6"
          style={editingImage ? undefined : { backgroundImage: 'radial-gradient(circle, color-mix(in oklch, var(--color-border) 55%, transparent) 0.5px, transparent 0.5px)', backgroundSize: '20px 20px' }}
        >
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
