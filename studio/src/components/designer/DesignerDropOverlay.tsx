import { Images } from 'lucide-react'

export interface DesignerDropOverlayProps {
  active: boolean
  incomingCount?: number
  remainingCapacity: number
}

function normalizeDisplayCount(value: number) {
  if (!Number.isFinite(value)) {
    return 0
  }

  return Math.max(0, Math.floor(value))
}

function actionCopyFor(incomingCount: number | undefined, remainingCapacity: number) {
  if (remainingCapacity === 0) {
    return '参考图已达上限，无法继续添加'
  }

  if (incomingCount === 0) {
    return '未检测到可添加的图片'
  }

  if (incomingCount !== undefined && incomingCount > remainingCapacity) {
    return `检测到 ${incomingCount} 张参考图，将仅添加前 ${remainingCapacity} 张`
  }

  if (incomingCount !== undefined) {
    return `释放以添加 ${incomingCount} 张参考图`
  }

  return '释放以添加参考图'
}

/** The consumer must provide a positioned containing block for this absolute overlay. */
export default function DesignerDropOverlay({
  active,
  incomingCount,
  remainingCapacity,
}: DesignerDropOverlayProps) {
  if (!active) {
    return null
  }

  const normalizedRemainingCapacity = normalizeDisplayCount(remainingCapacity)
  const normalizedIncomingCount = incomingCount === undefined
    ? undefined
    : normalizeDisplayCount(incomingCount)
  const actionCopy = actionCopyFor(
    normalizedIncomingCount,
    normalizedRemainingCapacity,
  )
  const capacityCopy = `当前模型还可添加 ${normalizedRemainingCapacity} 张`

  return (
    <>
      <span
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="pointer-events-none sr-only"
      >
        {actionCopy}。{capacityCopy}
      </span>
      <div
        data-testid="designer-drop-overlay"
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 z-50 p-3 sm:p-5 lg:p-7"
      >
        <div className="flex h-full w-full animate-in items-center justify-center rounded-3xl border-2 border-dashed border-primary bg-gradient-to-br from-background/90 via-card/80 to-chart-2/15 shadow-[inset_0_0_36px_-24px_var(--color-primary),0_24px_70px_-32px_var(--color-chart-2)] ring-1 ring-inset ring-primary/30 backdrop-blur-xl duration-150 fade-in-0 zoom-in-95 motion-reduce:animate-none">
          <div className="flex max-w-md flex-col items-center gap-4 rounded-2xl border border-primary/25 bg-card px-6 py-5 text-center text-card-foreground shadow-[0_18px_50px_-24px_var(--color-primary)] sm:px-8 sm:py-6">
            <div className="flex size-14 items-center justify-center rounded-2xl bg-primary/15 text-primary shadow-sm ring-1 ring-primary/25">
              <Images className="size-7" aria-hidden="true" />
            </div>
            <div className="space-y-1.5">
              <p className="text-base font-semibold text-foreground sm:text-lg">
                {actionCopy}
              </p>
              <p className="text-sm text-muted-foreground">
                {capacityCopy}
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}
