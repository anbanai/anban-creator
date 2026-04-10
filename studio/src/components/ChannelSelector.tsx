import { useState, useEffect } from 'react'
import { api, type Channel } from '@/lib/api'

interface ChannelSelectorProps {
  value: string
  onChange: (channelId: string, platform: string) => void
  platform?: string
}

const platformLabels: Record<string, string> = {
  article: '公众号',
  xls: '小绿书',
  rednote: '小红书',
}

export function ChannelSelector({ value, onChange, platform }: ChannelSelectorProps) {
  const [channels, setChannels] = useState<Channel[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.channels.list({ status: 'active', platform }).then(data => {
      setChannels(data)
      setLoading(false)
    }).catch(() => setLoading(false))
  }, [platform])

  if (loading) {
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
