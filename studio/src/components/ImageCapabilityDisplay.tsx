import type { ImageCapabilityOption } from '@/types/imageModel'

export function ImageCapabilityDisplay({ option, fallback = '已停用能力' }: { option?: ImageCapabilityOption; fallback?: string }) {
  if (!option) return <span className="text-muted-foreground">{fallback}</span>
  return (
    <span className="inline-flex flex-col gap-0.5">
      <span>{option.display_name}</span>
      {option.description && <span className="text-xs text-muted-foreground">{option.description}</span>}
      {typeof option.price_credits === 'number' && <span className="text-xs text-muted-foreground">{option.price_credits.toLocaleString()} 积分</span>}
    </span>
  )
}
