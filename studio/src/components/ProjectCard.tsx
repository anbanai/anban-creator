import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { Project, ProjectStats } from '@/types'
import { platformLabels } from '@/lib/labels'
import { renderPlatformIcon, platformBadgeVariant, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import { createTaskHref } from '@/lib/command-center'

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

export function ProjectCard({ project, stats, onEdit, archiving, restoring, onArchive, onRestore, onDelete }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const platformLabel = platformLabels[project.platform] || project.platform
  const platformBadge = platformBadgeVariant[project.platform] || ('secondary' as const)
  const borderColor = platformBorderColor[project.platform] || ''
  const hoverBorderColor = platformHoverBorderColor[project.platform] || ''
  const positioning = project.instructions || project.positioning || ''
  const taskHref = createTaskHref({ type: project.platform, projectId: project.id, intent: 'new' })
  const planHref = `/plans?create=true&type=${project.platform}&project_id=${project.id}&intent=schedule`
  const canCreatePlan = project.platform !== 'ecommerce'

  return (
    <div className={`group rounded-lg border border-border bg-card p-5 border-l-4 ${borderColor} ${hoverBorderColor} transition-all duration-200 hover:shadow-md active:scale-[0.98]`}>
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
      {stats && (
        <div className="mt-4 flex gap-4 border-t border-border pt-3 text-xs text-muted-foreground">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex flex-wrap gap-1.5">
        <Button variant="outline" size="xs" render={<Link to={taskHref} />}>
          新建任务
        </Button>
        {canCreatePlan && (
          <Button variant="ghost" size="xs" render={<Link to={planHref} />}>
            创建计划
          </Button>
        )}
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
