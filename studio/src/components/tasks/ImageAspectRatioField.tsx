import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

export type ImageAspectRatio = 'auto' | '3:4' | '1:1' | '4:3' | '16:9'

export function ImageAspectRatioField({
  value,
  defaultValue,
  ratios,
  onChange,
}: {
  value: ImageAspectRatio
  defaultValue?: string
  ratios: string[]
  onChange: (value: ImageAspectRatio) => void
}) {
  return (
    <ToggleGroup
      value={[value]}
      onValueChange={(next) => {
        const selected = next[0] as ImageAspectRatio | undefined
        if (selected) onChange(selected)
      }}
      aria-label="图片比例"
      variant="outline"
      size="sm"
      className="flex w-full flex-wrap justify-start"
    >
      <ToggleGroupItem value="auto">智能适配</ToggleGroupItem>
      {ratios.map((ratio) => (
        <ToggleGroupItem
          key={ratio}
          value={ratio}
          aria-label={`${ratio}${ratio === defaultValue ? ' 默认' : ''}`}
        >
          {ratio}{ratio === defaultValue ? ' 默认' : ''}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
