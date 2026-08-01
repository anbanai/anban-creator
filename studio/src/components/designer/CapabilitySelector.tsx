import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import type { DesignerCapability } from '@/types/designer'

interface CapabilitySelectorProps {
  capabilities: DesignerCapability[]
  selectedCapabilityKey: string
  onChange: (key: string) => void
  className?: string
}

export default function CapabilitySelector({ capabilities, selectedCapabilityKey, onChange, className }: CapabilitySelectorProps) {
  const options = capabilities.filter((capability) => capability.enabled && capability.priceAvailable === true).map((capability) => ({
    key: capability.id,
    display_name: capability.name,
    description: capability.description,
    price_credits: capability.credits,
    price_available: capability.priceAvailable,
    sort_order: capability.idx + 1,
    min_tier: capability.minTier,
    enabled: capability.enabled,
    features: {
      quality_levels: capability.features.qualityLevels,
      size_presets: capability.features.sizePresets,
      default_size: capability.features.defaultSize,
      max_batch: capability.features.maxBatch,
      max_reference_images: capability.features.maxReferenceImages,
      supports_reference: capability.features.supportsReference,
      supports_mask: capability.features.supportsMask,
      output_formats: capability.features.outputFormats,
      has_background: capability.features.hasBackground,
      has_compression: capability.features.hasCompression,
      watermark: capability.features.watermark,
    },
  }))

  if (options.length === 0) {
    return <p className="text-sm text-muted-foreground">暂无可用图像能力</p>
  }
  return <ImageCapabilitySelector options={options} value={selectedCapabilityKey} onChange={onChange} className={className} />
}
