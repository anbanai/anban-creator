import { useState, useCallback, useRef, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import DesignerTopBar from '@/components/designer/DesignerTopBar'
import DesignerToolbar from '@/components/designer/DesignerToolbar'
import DesignerCanvas from '@/components/designer/DesignerCanvas'
import DesignerPromptBar from '@/components/designer/DesignerPromptBar'
import HistoryDrawer from '@/components/designer/HistoryDrawer'
import ImagePreview from '@/components/designer/ImagePreview'
import type { DesignerSettings } from '@/types/designer'
import { api } from '@/lib/api'
import { designerApi } from '@/lib/api/designer'
import { getApiErrorMessage } from '@/lib/http-client'
import type { GenerateImage, ImageGeneration, ImageGenerationResult, DesignerProvider } from '@/types/designer'

const DEFAULT_SETTINGS: DesignerSettings = {
  quality: 'auto',
  size: '1:1',
  n: 1,
  outputFormat: 'png',
  referenceFiles: [],
  maskFile: null,
}

const POLL_INTERVAL = 2000
const MAX_POLLS = 90 // 3 minutes at 2s interval
const MAX_CONSECUTIVE_ERRORS = 3

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
  const [selectedGenerationId, setSelectedGenerationId] = useState<string>()
  const [previewImage, setPreviewImage] = useState<string | null>(null)
  const [isGenerating, setIsGenerating] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
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
    queryFn: () => api.designer.getProviders(),
  })

  const providerList: DesignerProvider[] = providers ?? []

  // Auto-select first provider if none selected or current selection is unavailable
  const activeProvider = providerList.find((p) => p.id === selectedProviderId)
  const effectiveProvider = activeProvider ?? providerList[0]

  function stopPolling() {
    if (pollingRef.current) {
      clearInterval(pollingRef.current)
      pollingRef.current = null
    }
  }

  const handleGenerate = useCallback(async (prompt: string) => {
    if (!effectiveProvider) {
      toast.error('没有可用的图片模型')
      return
    }

    // Cancel any in-progress polling
    stopPolling()
    abortedRef.current = false

    setIsGenerating(true)
    setCurrentImages([])

    try {
      // Upload reference files if any
      const refFileIds: string[] = []
      for (const file of settings.referenceFiles) {
        const result = await api.designer.uploadReference(file)
        refFileIds.push(result.file_id)
      }

      // Upload mask if present
      let maskFileId: string | undefined
      if (settings.maskFile) {
        const result = await api.designer.uploadReference(settings.maskFile)
        maskFileId = result.file_id
      }

      // Start async generation
      const { generation_id } = await designerApi.generate({
        channel_id: '',
        prompt,
        provider: effectiveProvider.provider,
        quality: settings.quality !== 'auto' ? settings.quality : undefined,
        size: settings.size,
        n: settings.n > 1 ? settings.n : undefined,
        output_format: settings.outputFormat !== 'png' ? settings.outputFormat : undefined,
        reference_file_ids: refFileIds.length > 0 ? refFileIds : undefined,
        mask_file_id: maskFileId,
      })

      setSelectedGenerationId(generation_id)

      // Poll for completion
      let pollCount = 0
      let consecutiveErrors = 0

      pollingRef.current = setInterval(async () => {
        if (abortedRef.current) {
          stopPolling()
          setIsGenerating(false)
          return
        }

        pollCount++
        if (pollCount >= MAX_POLLS) {
          stopPolling()
          setIsGenerating(false)
          toast.error('生成超时，请稍后在历史记录中查看结果')
          return
        }

        try {
          const gen = await designerApi.getGeneration(generation_id)
          consecutiveErrors = 0

          if (gen.status === 'completed') {
            stopPolling()
            setIsGenerating(false)
            setCurrentImages(resultsToImages(gen.results))
            toast.success('图片生成成功')
            queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
          } else if (gen.status === 'failed') {
            stopPolling()
            setIsGenerating(false)
            toast.error(gen.error || '图片生成失败')
          }
        } catch {
          consecutiveErrors++
          if (consecutiveErrors >= MAX_CONSECUTIVE_ERRORS) {
            stopPolling()
            setIsGenerating(false)
            toast.error('查询生成状态失败')
          }
        }
      }, POLL_INTERVAL)
    } catch (err) {
      setIsGenerating(false)
      toast.error(getApiErrorMessage(err, '图片生成失败，请重试'))
    }
  }, [effectiveProvider, settings, queryClient])

  const handleHistorySelect = useCallback((gen: ImageGeneration) => {
    setSelectedGenerationId(gen.id)
    setCurrentImages(resultsToImages(gen.results))
  }, [])

  function handleModelChange(providerId: string) {
    setSelectedProviderId(providerId)
    setSettings(DEFAULT_SETTINGS)
  }

  function handleCancel() {
    abortedRef.current = true
    stopPolling()
    setIsGenerating(false)
  }

  return (
    // Full-bleed: negate AppLayout padding (px-4 py-6 md:px-8 md:py-8)
    <div className="-mx-4 -my-6 flex flex-col overflow-hidden md:-mx-8 md:-my-8" style={{ height: '100dvh' }}>
      {/* Top bar */}
      <DesignerTopBar
        providers={providerList}
        selectedProviderId={effectiveProvider?.id ?? ''}
        onModelChange={handleModelChange}
        provider={effectiveProvider?.provider ?? ''}
        settings={settings}
        onSettingsChange={setSettings}
      />

      {/* Main content: flex-col on mobile, flex-row on desktop */}
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        {/* Toolbar: order-2 on mobile (bottom), order-first on desktop (left) */}
        <div className="order-2 md:order-first">
          <DesignerToolbar
            provider={effectiveProvider?.provider ?? ''}
            settings={settings}
            onSettingsChange={setSettings}
            onHistoryToggle={() => setHistoryOpen(true)}
          />
        </div>

        {/* Center: Canvas + PromptBar */}
        <div className="order-1 flex min-h-0 flex-1 flex-col">
          <div className="flex-1 overflow-y-auto p-4">
            <DesignerCanvas
              images={currentImages}
              isGenerating={isGenerating}
              onImageClick={(img) => setPreviewImage(img.url)}
            />
          </div>
          <DesignerPromptBar
            onSubmit={handleGenerate}
            isGenerating={isGenerating}
            onCancel={handleCancel}
            providers={providerList}
            selectedProviderId={effectiveProvider?.id ?? ''}
            onModelChange={handleModelChange}
          />
        </div>
      </div>

      {/* History drawer (portal-based, no layout impact) */}
      <HistoryDrawer
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        onSelect={handleHistorySelect}
        selectedId={selectedGenerationId}
      />

      {/* Image preview modal */}
      {previewImage && (
        <ImagePreview
          imageUrl={previewImage}
          onClose={() => setPreviewImage(null)}
        />
      )}
    </div>
  )
}
