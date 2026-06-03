import { useState, useCallback } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
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
import { getApiErrorMessage } from '@/lib/http-client'
import { DESIGNER_MODELS } from '@/types/designer'
import type { GenerateImage, ImageGeneration } from '@/types/designer'

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
  const [selectedModel, setSelectedModel] = useState(DESIGNER_MODELS[0].id)
  const [settings, setSettings] = useState<DesignerSettings>(DEFAULT_SETTINGS)
  const [currentImages, setCurrentImages] = useState<GenerateImage[]>([])
  const [selectedGenerationId, setSelectedGenerationId] = useState<string>()
  const [previewImage, setPreviewImage] = useState<string | null>(null)

  const generateMutation = useMutation({
    mutationFn: async (prompt: string) => {
      const modelDef = DESIGNER_MODELS.find((m) => m.id === selectedModel)
      if (!modelDef) throw new Error('未选择模型')

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

      // Determine provider from model definition
      return api.designer.generate({
        channel_id: '',
        prompt,
        provider: modelDef.provider,
        model: selectedModel,
        quality: settings.quality !== 'auto' ? settings.quality : undefined,
        size: settings.size,
        n: settings.n > 1 ? settings.n : undefined,
        output_format: settings.outputFormat !== 'png' ? settings.outputFormat : undefined,
        reference_file_ids: refFileIds.length > 0 ? refFileIds : undefined,
        mask_file_id: maskFileId,
      })
    },
    onSuccess: (result) => {
      setCurrentImages(result.images)
      setSelectedGenerationId(result.generation_id)
      toast.success('图片生成成功')
      queryClient.invalidateQueries({ queryKey: ['designer', 'history'] })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '图片生成失败，请重试'))
    },
  })

  const handleGenerate = useCallback((prompt: string) => {
    generateMutation.mutate(prompt)
  }, [generateMutation])

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

  function handleModelChange(modelId: string) {
    setSelectedModel(modelId)
    // Reset settings to defaults when model changes
    setSettings(DEFAULT_SETTINGS)
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <PageHeader title="设计师" description="AI 图片生成工作室，支持多种模型和风格。" />

      {/* Model selector */}
      <ModelSelector selectedModel={selectedModel} onModelChange={handleModelChange} />

      {/* Main content area */}
      <div className="flex min-h-0 flex-1 gap-4">
        {/* Left: Settings */}
        <div className="hidden md:block">
          <SettingsPanel
            model={selectedModel}
            settings={settings}
            onSettingsChange={setSettings}
          />
        </div>

        {/* Center: Generation grid */}
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <div className="flex-1">
            <GenerationGrid
              images={currentImages}
              isGenerating={generateMutation.isPending}
              onImageClick={(img) => setPreviewImage(img.url)}
            />
          </div>
          <PromptInput
            onSubmit={handleGenerate}
            isGenerating={generateMutation.isPending}
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
