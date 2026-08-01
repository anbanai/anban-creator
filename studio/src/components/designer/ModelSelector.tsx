import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import type { DesignerProvider } from '@/types/designer'

interface ModelSelectorProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onChange: (id: string) => void
  className?: string
}

export default function ModelSelector({ providers, selectedProviderId, onChange, className }: ModelSelectorProps) {
  const options = providers.filter((provider) => provider.enabled && provider.priceAvailable !== false).map((provider) => ({
    key: provider.id,
    display_name: provider.name,
    description: provider.description,
    price_credits: provider.credits,
    sort_order: provider.idx + 1,
    min_tier: provider.minTier,
  }))

  return (
    <ImageCapabilitySelector
      options={options}
      value={selectedProviderId}
      onChange={onChange}
      className={className}
    />
  )
}
