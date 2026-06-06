import { useState, useCallback, useRef, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import PageHeader from '@/components/layout/PageHeader'
import ModelSelector from '@/components/designer/ModelSelector'
import SettingsPanel from '@/components/designer/SettingsPanel'
import type { DesignerSettings } from '@/components/designer/SettingsPanel'
import GenerationGrid from '@/components/designer/GenerationGrid'
import PromptInput from '@/components/designer/PromptInput'
import HistorySidebar from '@/components/designer/HistorySidebar'
import ImagePreview from '@/components/designer/ImagePreview'
import { api } from '@/lib/api'
import { generateStream } from '@/lib/api/designer'
import { getApiErrorMessage } from '@/lib/http-client'
import type { GenerateImage, ImageGeneration, DesignerProvider, GenerateResult } from '@/types/designer'

const DEFAULT_SETTINGS: DesignerSettings = {
  quality: 'auto',
  size: '1:1',
  n: 1,
  outputFormat: 'png',
  referenceFiles: [],
  maskFile: null,
}

export default function DesignerPage() {
  const queryClient = useQueryClient()
  const [selectedProviderId, setSelectedProviderId] = useState<string>('')
  const [settings, setSettings] = useState<DesignerSettings>(DEFAULT_SETTINGS)
  const [currentImages, setCurrentImages] = useState<GenerateImage[]>([])
  const [selectedGenerationId, setSelectedGenerationId] = useState<string>()
  const [previewImage, setPreviewImage] = useState<string | null>(null)
  const [isGenerating, setIsGenerating] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  // Abort in-progress generation on unmount
  useEffect(() => {
    return () => { abortRef.current?.abort() }
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

  const handleGenerate = useCallback(async (prompt: string) => {
    if (!effectiveProvider) {
      toast.error('没有可用的图片模型')
      return
    }

    // Cancel any in-progress generation
    abortRef.current?.abort()
    const abort = new AbortController()
    abortRef.current = abort

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

      // Stream generation via SSE
      const stream = generateStream({
        channel_id: '',
        prompt,
        provider: effectiveProvider.provider,
        quality: settings.quality !== 'auto' ? settings.quality : undefined,
        size: settings.size,
        n: settings.n > 1 ? settings.n : undefined,
        output_format: settings.outputFormat !== 'png' ? settings.outputFormat : undefined,
        reference_file_ids: refFileIds.length > 0 ? refFileIds : undefined,
        mask_file_id: maskFileId,
      }, abort.signal)

      for await (const event of stream) {
        if (abort.signal.aborted) break

        switch (event.event) {
          case 'result': {
            let result: GenerateResult
            try { result = JSON.parse(event.data) } catch { throw new Error('服务端返回数据异常') }
            setCurrentImages(result.images)
            setSelectedGenerationId(result.generation_id)
            toast.success('图片生成成功')
            queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
            break
          }
          case 'error': {
            let errMsg = '图片生成失败'
            try { errMsg = JSON.parse(event.data).error || errMsg } catch {}
            throw new Error(errMsg)
          }
        }
      }
    } catch (err) {
      if (abort.signal.aborted) return
      toast.error(getApiErrorMessage(err, '图片生成失败，请重试'))
    } finally {
      if (abortRef.current === abort) {
        setIsGenerating(false)
        abortRef.current = null
      }
    }
  }, [effectiveProvider, settings, queryClient])

  const handleHistorySelect = useCallback((gen: ImageGeneration) => {
    setSelectedGenerationId(gen.id)
    if (gen.results && gen.results.length > 0) {
      const images: GenerateImage[] = gen.results
        .filter((r) => r.image_url || r.image_path)
        .map((r, i) => ({
          url: r.image_url || r.image_path!,
          width: r.width,
          height: r.height,
          index: i,
        }))
      setCurrentImages(images)
    }
  }, [])

  function handleModelChange(providerId: string) {
    setSelectedProviderId(providerId)
    setSettings(DEFAULT_SETTINGS)
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <PageHeader title="设计师" description="AI 图片生成工作室，支持多种模型和风格。" />

      {/* Model selector */}
      <ModelSelector
        providers={providerList}
        selectedId={effectiveProvider?.id ?? ''}
        onModelChange={handleModelChange}
      />

      {/* Main content area */}
      <div className="flex min-h-0 flex-1 gap-4">
        {/* Left: Settings */}
        <div className="hidden md:block">
          <SettingsPanel
            provider={effectiveProvider?.provider ?? ''}
            settings={settings}
            onSettingsChange={setSettings}
          />
        </div>

        {/* Center: Generation grid */}
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <div className="flex-1">
            <GenerationGrid
              images={currentImages}
              isGenerating={isGenerating}
              onImageClick={(img) => setPreviewImage(img.url)}
            />
          </div>
          <PromptInput
            onSubmit={handleGenerate}
            isGenerating={isGenerating}
          />
        </div>

        {/* Right: History */}
        <div className="hidden lg:block">
          <HistorySidebar
            onSelect={handleHistorySelect}
            selectedId={selectedGenerationId}
          />
        </div>
      </div>

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
