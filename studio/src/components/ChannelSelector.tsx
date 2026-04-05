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
    return <div className="h-10 animate-pulse rounded-lg bg-gray-700" />
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
        const ch = channels.find(c => c.id === e.target.value)
        if (ch) onChange(ch.id, ch.platform)
      }}
      className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-gray-100 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
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
