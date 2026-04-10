import { type Channel, type ChannelStats } from '@/lib/api'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'

interface ChannelCardProps {
  channel: Channel
  stats?: ChannelStats
  onEdit?: (channel: Channel) => void
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
  onDelete?: (id: string) => void
}

const platformLabels: Record<string, string> = {
  article: '公众号',
  xls: '小绿书',
  rednote: '小红书',
}

const platformBadgeVariant: Record<string, 'success' | 'info' | 'danger' | 'neutral'> = {
  article: 'success',
  xls: 'info',
  rednote: 'danger',
}

export function ChannelCard({ channel, stats, onEdit, onArchive, onRestore, onDelete }: ChannelCardProps) {
  const platformLabel = platformLabels[channel.platform] || channel.platform
  const platformBadge = platformBadgeVariant[channel.platform] || ('neutral' as const)

  return (
    <div className="group rounded-lg border border-border bg-card p-5 transition-colors duration-150 hover:border-primary/30">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          {channel.avatar_url ? (
            <img
              src={channel.avatar_url}
              alt={channel.name}
              className="h-11 w-11 rounded-full object-cover ring-1 ring-border"
            />
          ) : (
            <div className="flex h-11 w-11 items-center justify-center rounded-full bg-primary/10 text-sm font-semibold text-primary">
              {channel.name.charAt(0)}
            </div>
          )}
          <div>
            <h3 className="text-sm font-semibold text-foreground">{channel.name}</h3>
            <Badge variant={platformBadge} className="mt-1 text-[10px]">
              {platformLabel}
            </Badge>
          </div>
        </div>
        {channel.status === 'archived' && (
          <Badge variant="warning" className="text-[10px]">已归档</Badge>
        )}
      </div>
      {channel.positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{channel.positioning}</p>
      )}
      {stats && (
        <div className="mt-4 flex gap-4 border-t border-border pt-3 text-xs text-muted-foreground">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex gap-1.5">
        {onEdit && (
          <Button variant="ghost" size="xs" onClick={() => onEdit(channel)}>
            编辑
          </Button>
        )}
        {channel.status === 'active' && onArchive && (
          <Button variant="ghost" size="xs" onClick={() => onArchive(channel.id)}>
            归档
          </Button>
        )}
        {channel.status === 'archived' && onRestore && (
          <Button variant="ghost" size="xs" onClick={() => onRestore(channel.id)}>
            恢复
          </Button>
        )}
        {onDelete && (
          <Button variant="danger" size="xs" onClick={() => onDelete(channel.id)}>
            删除
          </Button>
        )}
      </div>
    </div>
  )
}
