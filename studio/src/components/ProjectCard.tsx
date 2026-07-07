import { useState } from 'react'
import type { Project, ProjectStats } from '@/types'
import { platformLabels } from '@/lib/labels'
import { renderPlatformIcon, platformBadgeVariant, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'

interface ProjectCardProps {
  project: Project
  stats?: ProjectStats
  onEdit?: (project: Project) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
  onDelete?: (id: string) => void
}

function buildOperatingBadges(project: Project, stats?: ProjectStats) {
  const badges: Array<{ label: string; variant: 'secondary' | 'outline' | 'destructive' }> = []

  if (project.config.enable_publishing) {
    badges.push({ label: '公众号草稿箱', variant: 'secondary' })
    badges.push({
      label: project.config.require_publish_approval ? '发布需审核' : '自动入草稿箱',
      variant: project.config.require_publish_approval ? 'outline' : 'secondary',
    })
  } else if (project.platform === 'article') {
    badges.push({ label: '发布未启用', variant: 'outline' })
  }

  badges.push({
    label: project.visual_style ? '视觉已配置' : '视觉未配置',
    variant: project.visual_style ? 'secondary' : 'outline',
  })

  if (project.platform === 'article') {
    badges.push({
      label: project.writer || project.theme || project.author ? '写作已配置' : '写作未配置',
      variant: project.writer || project.theme || project.author ? 'secondary' : 'outline',
    })
  }

  if (project.platform === 'ecommerce' && project.ecommerce_defaults?.target_platform) {
    badges.push({ label: `投放 ${project.ecommerce_defaults.target_platform}`, variant: 'secondary' })
  }

  if (project.platform === 'video' && project.video_defaults?.model_key) {
    badges.push({ label: '视频默认已配置', variant: 'secondary' })
  }

  if (stats && stats.total_tasks > 0) {
    badges.push({
      label: `成功率 ${(stats.success_rate * 100).toFixed(0)}%`,
      variant: stats.success_rate >= 0.6 ? 'secondary' : 'destructive',
    })
  }

  return badges
}

export function ProjectCard({ project, stats, onEdit, archiving, restoring, onArchive, onRestore, onDelete }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const platformLabel = platformLabels[project.platform] || project.platform
  const platformBadge = platformBadgeVariant[project.platform] || ('secondary' as const)
  const borderColor = platformBorderColor[project.platform] || ''
  const hoverBorderColor = platformHoverBorderColor[project.platform] || ''
  const positioning = project.instructions || project.positioning || ''
  const operatingBadges = buildOperatingBadges(project, stats)
  const isArchived = project.status === 'archived'
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30 opacity-75 grayscale-[0.25] hover:border-border/70 hover:shadow-none'
    : `border-border bg-card ${borderColor} ${hoverBorderColor} hover:shadow-md`

  return (
    <div className={`group rounded-lg border border-l-4 p-5 transition-all duration-200 active:scale-[0.98] ${cardTone}`}>
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <PlatformAvatar avatarUrl={project.avatar_url} name={project.name} platform={project.platform} size="lg" />
          <div>
            <h3 className="text-sm font-semibold text-foreground">{project.name}</h3>
            <Badge variant={platformBadge} className="mt-1 text-[10px]">
              {renderPlatformIcon(project.platform)}
              {platformLabel}
            </Badge>
          </div>
        </div>
        {project.status === 'archived' && (
          <Badge variant="outline" className="text-[10px]">已归档</Badge>
        )}
      </div>
      {positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{positioning}</p>
      )}
      {operatingBadges.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {operatingBadges.map((badge) => (
            <Badge key={badge.label} variant={badge.variant} className="text-[10px]">
              {badge.label}
            </Badge>
          ))}
        </div>
      )}
      {stats && (
        <div className="mt-4 flex gap-4 border-t border-border pt-3 text-xs text-muted-foreground">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex flex-wrap gap-1.5">
        {onEdit && (
          <Button variant="ghost" size="xs" onClick={() => onEdit(project)} aria-label="编辑项目">
            编辑
          </Button>
        )}
        {project.status === 'active' && onArchive && (
          <Button variant="ghost" size="xs" disabled={archiving} onClick={() => onArchive(project.id)} aria-label="归档项目">
            归档
          </Button>
        )}
        {project.status === 'archived' && onRestore && (
          <Button variant="ghost" size="xs" disabled={restoring} onClick={() => onRestore(project.id)} aria-label="恢复项目">
            恢复
          </Button>
        )}
        {onDelete && (
          <Button variant="destructive" size="xs" onClick={() => onDelete(project.id)} aria-label="删除项目">
            删除
          </Button>
        )}
        <Button variant="ghost" size="xs" onClick={() => setTopicPoolOpen(true)} aria-label="查看素材/选题池">
          查看素材/选题池
        </Button>
      </div>
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
    </div>
  )
}
