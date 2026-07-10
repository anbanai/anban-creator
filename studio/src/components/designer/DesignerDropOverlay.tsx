import { Images } from 'lucide-react'

export interface DesignerDropOverlayProps {
  active: boolean
  incomingCount?: number
  remainingCapacity: number
}

export default function DesignerDropOverlay({
  active,
  incomingCount,
  remainingCapacity,
}: DesignerDropOverlayProps) {
  if (!active) {
    return null
  }

  const actionCopy = incomingCount !== undefined && incomingCount > 0
    ? `释放以添加 ${incomingCount} 张参考图`
    : '释放以添加参考图'

  return (
    <div
      data-testid="designer-drop-overlay"
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-50 p-3 sm:p-5 lg:p-7"
    >
      <div className="flex h-full w-full animate-in items-center justify-center rounded-3xl border-2 border-dashed border-primary/70 bg-gradient-to-br from-background/90 via-card/80 to-chart-2/15 shadow-[inset_0_0_36px_-24px_var(--color-primary),0_24px_70px_-32px_var(--color-chart-2)] backdrop-blur-xl duration-150 fade-in-0 zoom-in-95 motion-reduce:animate-none">
        <div className="flex flex-col items-center gap-4 px-6 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-primary/15 text-primary shadow-sm ring-1 ring-primary/25">
            <Images className="size-7" aria-hidden="true" />
          </div>
          <div className="space-y-1.5">
            <p className="text-base font-semibold text-foreground sm:text-lg">
              {actionCopy}
            </p>
            <p className="text-sm text-muted-foreground">
              当前模型还可添加 {remainingCapacity} 张
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
