import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn } from '@/lib/utils'

export type ImageAspectRatio = '3:4' | '1:1' | '4:3' | '16:9' | '3:2' | '2:3' | '9:16' | '21:9'

const imageAspectRatioOptions: ReadonlyArray<{
  value: ImageAspectRatio
  accessibleOrientation: string
  orientation: string
  previewClassName: string
}> = [
  {
    value: '3:4',
    accessibleOrientation: 'vertical',
    orientation: '竖版',
    previewClassName: 'h-12 aspect-[3/4]',
  },
  {
    value: '1:1',
    accessibleOrientation: 'square',
    orientation: '方形',
    previewClassName: 'h-12 aspect-square',
  },
  {
    value: '4:3',
    accessibleOrientation: 'horizontal',
    orientation: '横版',
    previewClassName: 'h-10 aspect-[4/3]',
  },
  {
    value: '16:9',
    accessibleOrientation: 'widescreen',
    orientation: '宽屏',
    previewClassName: 'h-9 aspect-video',
  },
  { value: '3:2', accessibleOrientation: 'horizontal', orientation: '横版', previewClassName: 'h-9 aspect-[3/2]' },
  { value: '2:3', accessibleOrientation: 'vertical', orientation: '竖版', previewClassName: 'h-12 aspect-[2/3]' },
  { value: '9:16', accessibleOrientation: 'vertical', orientation: '竖屏', previewClassName: 'h-12 aspect-[9/16]' },
  { value: '21:9', accessibleOrientation: 'ultrawide', orientation: '超宽', previewClassName: 'h-7 aspect-[21/9]' },
]

export function ImageAspectRatioField({
  value,
  defaultValue,
  supportedSizes,
  onChange,
}: {
  value: '' | ImageAspectRatio
  defaultValue: string
  supportedSizes?: string[]
  onChange: (value: '' | ImageAspectRatio) => void
}) {
  const supported = supportedSizes ? new Set(supportedSizes) : null
  return (
    <RadioGroup
      value={value}
      onValueChange={(nextValue) => onChange(nextValue as '' | ImageAspectRatio)}
      aria-label="图片比例"
      className="grid-cols-2 gap-2 overflow-x-clip sm:grid-cols-3 lg:grid-cols-5"
    >
      <label
        aria-label="智能适配"
        className={cn(
          'flex min-h-24 cursor-pointer flex-col justify-between gap-2 rounded-md border border-border bg-background p-2 text-sm transition-colors hover:bg-accent/50',
          value === '' && 'border-primary bg-accent',
        )}
      >
        <span className="flex items-center justify-between gap-2">
          <span className="font-medium text-foreground">智能适配</span>
          <RadioGroupItem value="" aria-label="智能适配" />
        </span>
        <span className="text-xs text-muted-foreground">按每张产物选择合适比例</span>
      </label>
      {imageAspectRatioOptions.map((option) => {
        const isDefault = option.value === defaultValue
        const disabled = supported !== null && !supported.has(option.value)
        const accessibleName = `${option.value} ${option.accessibleOrientation}${isDefault ? ' default' : ''}`

        return (
          <label
            key={option.value}
            aria-label={accessibleName}
            className={cn(
              'flex min-h-24 cursor-pointer flex-col justify-between gap-2 rounded-md border border-border bg-background p-2 text-sm transition-colors hover:bg-accent/50',
              value === option.value && 'border-primary bg-accent',
              disabled && 'cursor-not-allowed opacity-45 hover:bg-background',
            )}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="font-medium text-foreground">{option.value}</span>
              <RadioGroupItem value={option.value} aria-label={accessibleName} disabled={disabled} />
            </span>
            <span className="flex items-end justify-between gap-2">
              <span className={cn('shrink-0 border border-border bg-muted/50', option.previewClassName)} />
              <span className="min-w-0 text-right text-xs text-muted-foreground">
                {option.orientation}
                {isDefault && ' 默认'}
              </span>
            </span>
          </label>
        )
      })}
    </RadioGroup>
  )
}
