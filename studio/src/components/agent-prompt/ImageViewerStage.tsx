import { useEffect, useState } from 'react'
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  Maximize2Icon,
  MinusIcon,
  PlusIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

const MIN_ZOOM = 25
const MAX_ZOOM = 400
const ZOOM_STEP = 25
const FIT_ZOOM = 100

export interface ImageViewerStageProps {
  src: string
  alt: string
  index?: number
  count?: number
  onPrevious?: () => void
  onNext?: () => void
  onError?: () => void
}

function IconButton({
  label,
  children,
  ...props
}: Omit<React.ComponentProps<typeof Button>, 'children'> & {
  label: string
  children: React.ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            aria-label={label}
            {...props}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

export function ImageViewerStage({
  src,
  alt,
  index = 0,
  count = 1,
  onPrevious,
  onNext,
  onError,
}: ImageViewerStageProps) {
  const [zoom, setZoom] = useState(FIT_ZOOM)

  useEffect(() => {
    setZoom(FIT_ZOOM)
  }, [src])

  const hasNavigation = count > 1
  const canPrevious = index > 0
  const canNext = index < count - 1

  return (
    <div
      data-slot="image-viewer-stage"
      className="flex min-h-0 w-full flex-1 flex-col gap-3"
    >
      <div className="relative flex min-h-64 flex-1 items-center justify-center overflow-auto rounded-md bg-muted/40 p-4 sm:min-h-80">
        <img
          src={src}
          alt={alt}
          onError={onError}
          className="max-h-[62vh] max-w-full object-contain transition-transform"
          style={{ transform: `scale(${zoom / 100})` }}
        />

        {hasNavigation ? (
          <>
            <div className="absolute inset-y-0 left-2 flex items-center">
              <IconButton label="上一项" disabled={!canPrevious} onClick={onPrevious}>
                <ChevronLeftIcon data-icon="inline-start" />
              </IconButton>
            </div>
            <div className="absolute inset-y-0 right-2 flex items-center">
              <IconButton label="下一项" disabled={!canNext} onClick={onNext}>
                <ChevronRightIcon data-icon="inline-start" />
              </IconButton>
            </div>
          </>
        ) : null}
      </div>

      <div className="flex min-w-0 flex-wrap items-center justify-center gap-2">
        <IconButton
          label="缩小"
          disabled={zoom <= MIN_ZOOM}
          onClick={() => setZoom((current) => Math.max(MIN_ZOOM, current - ZOOM_STEP))}
        >
          <MinusIcon data-icon="inline-start" />
        </IconButton>
        <Slider
          aria-label="缩放比例"
          value={[zoom]}
          min={MIN_ZOOM}
          max={MAX_ZOOM}
          step={ZOOM_STEP}
          onValueChange={(value) => setZoom(Number(Array.isArray(value) ? value[0] : value))}
          onKeyDown={(event) => {
            if (event.key === 'ArrowUp' || event.key === 'ArrowRight') {
              event.stopPropagation()
              setZoom((current) => Math.min(MAX_ZOOM, current + ZOOM_STEP))
            } else if (event.key === 'ArrowDown' || event.key === 'ArrowLeft') {
              event.stopPropagation()
              setZoom((current) => Math.max(MIN_ZOOM, current - ZOOM_STEP))
            }
          }}
          className="w-36 max-w-[40vw]"
        />
        <span className="w-12 text-center text-xs tabular-nums text-muted-foreground">
          {zoom}%
        </span>
        <IconButton
          label="放大"
          disabled={zoom >= MAX_ZOOM}
          onClick={() => setZoom((current) => Math.min(MAX_ZOOM, current + ZOOM_STEP))}
        >
          <PlusIcon data-icon="inline-start" />
        </IconButton>
        <IconButton label="适合窗口" onClick={() => setZoom(FIT_ZOOM)}>
          <Maximize2Icon data-icon="inline-start" />
        </IconButton>
      </div>
    </div>
  )
}
