import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { platformLabels } from '@/lib/labels'
import { Combobox, type ComboboxOption } from '@/components/Combobox'

interface ChannelSelectorProps {
  value: string
  onChange: (channelId: string, platform: string) => void
  platform?: string
}

export function ChannelSelector({ value, onChange, platform }: ChannelSelectorProps) {
  const { data: channels = [], isLoading } = useQuery({
    queryKey: ['channels', 'active', platform],
    queryFn: () => api.channels.list({ status: 'active', platform }),
  })

  const options: ComboboxOption[] = channels.map((ch) => ({
    value: ch.id,
    label: ch.name,
    group: platformLabels[ch.platform] || ch.platform,
  }))

  if (isLoading) {
    return <div className="h-8 animate-pulse rounded-lg bg-muted" />
  }

  return (
    <Combobox
      options={options}
      value={value}
      onChange={(id) => {
        if (!id) {
          onChange('', '')
          return
        }
        const ch = channels.find((c) => c.id === id)
        if (ch) onChange(ch.id, ch.platform)
      }}
      placeholder="选择频道..."
      searchPlaceholder="搜索频道..."
      emptyText="没有找到频道"
    />
  )
}
