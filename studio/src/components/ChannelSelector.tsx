import { useQuery } from '@tanstack/react-query'
import { api, type Channel } from '@/lib/api'
import { platformLabels } from '@/lib/labels'

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

  if (isLoading) {
    return <div className="h-10 animate-pulse rounded-lg bg-muted" />
  }

  // Group by platform
  const groups = channels.reduce((acc, ch) => {
    const label = platformLabels[ch.platform] || ch.platform
    if (!acc[label]) acc[label] = []
    acc[label].push(ch)
    return acc
  }, {} as Record<string, Channel[]>)

  return (
    <select
      value={value}
      onChange={e => {
        const val = e.target.value
        if (!val) { onChange('', ''); return }
        const ch = channels.find(c => c.id === val)
        if (ch) onChange(ch.id, ch.platform)
      }}
      className="w-full rounded-md border border-input bg-transparent px-2 py-1 text-sm text-foreground transition-colors focus:border-ring focus:outline-none dark:bg-input/30 dark:hover:bg-input/50"
    >
      <option value="">选择频道...</option>
      {Object.entries(groups).map(([label, items]) => (
        <optgroup key={label} label={label}>
          {items.map(ch => (
            <option key={ch.id} value={ch.id}>{ch.name}</option>
          ))}
        </optgroup>
      ))}
    </select>
  )
}
