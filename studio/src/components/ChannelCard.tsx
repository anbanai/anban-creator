import type { Channel, ChannelStats } from '@/types'

interface ChannelCardProps {
  channel: Channel
  stats?: ChannelStats
  onEdit?: (channel: Channel) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
  onDelete?: (id: string) => void
}

const platformLabels: Record<string, string> = {
  article: '公众号',
  xls: '小绿书',
  rednote: '小红书',
}

const platformColors: Record<string, string> = {
  article: 'bg-green-100 text-green-800',
  xls: 'bg-blue-100 text-blue-800',
  rednote: 'bg-red-100 text-red-800',
}

export function ChannelCard({ channel, stats, onEdit, archiving: _archiving, restoring: _restoring, onArchive, onRestore, onDelete }: ChannelCardProps) {
  const platformLabel = platformLabels[channel.platform] || channel.platform
  const platformColor = platformColors[channel.platform] || 'bg-gray-100 text-gray-800'

  return (
    <div className="rounded-lg border border-gray-200 p-4 transition-shadow hover:shadow-md">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          {channel.avatar_url ? (
            <img
              src={channel.avatar_url}
              alt={channel.name}
              className="h-12 w-12 rounded-full object-cover"
            />
          ) : (
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gray-200 text-lg font-medium text-gray-500">
              {channel.name.charAt(0)}
            </div>
          )}
          <div>
            <h3 className="font-medium text-gray-900">{channel.name}</h3>
            <span className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${platformColor}`}>
              {platformLabel}
            </span>
          </div>
        </div>
        {channel.status === 'archived' && (
          <span className="rounded bg-yellow-100 px-2 py-1 text-xs text-yellow-800">已归档</span>
        )}
      </div>
      {channel.positioning && (
        <p className="mt-2 line-clamp-2 text-sm text-gray-500">{channel.positioning}</p>
      )}
      {stats && (
        <div className="mt-3 flex gap-4 border-t border-gray-100 pt-3 text-xs text-gray-500">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex gap-2">
        {onEdit && (
          <button
            onClick={() => onEdit(channel)}
            className="rounded bg-gray-100 px-2 py-1 text-xs hover:bg-gray-200"
          >
            编辑
          </button>
        )}
        {channel.status === 'active' && onArchive && (
          <button
            onClick={() => onArchive(channel.id)}
            className="rounded bg-yellow-100 px-2 py-1 text-xs text-yellow-800 hover:bg-yellow-200"
          >
            归档
          </button>
        )}
        {channel.status === 'archived' && onRestore && (
          <button
            onClick={() => onRestore(channel.id)}
            className="rounded bg-green-100 px-2 py-1 text-xs text-green-800 hover:bg-green-200"
          >
            恢复
          </button>
        )}
        {onDelete && (
          <button
            onClick={() => onDelete(channel.id)}
            className="rounded bg-red-100 px-2 py-1 text-xs text-red-800 hover:bg-red-200"
          >
            删除
          </button>
        )}
      </div>
    </div>
  )
}
