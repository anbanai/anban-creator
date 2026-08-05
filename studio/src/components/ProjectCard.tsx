import { useState } from 'react'
import { Archive, Ellipsis, Lightbulb, Pencil, RotateCcw } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { platformLabels } from '@/lib/labels'
import { renderPlatformIcon, platformBadgeVariant } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

interface ProjectCardProps {
  project: Project
  stats?: ProjectStats
  onEdit?: (project: Project) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
}

export function ProjectCard({ project, stats, onEdit, archiving, restoring, onArchive, onRestore }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const platformLabel = platformLabels[project.platform] || project.platform
  const platformBadge = platformBadgeVariant[project.platform] || ('secondary' as const)
  const positioning = project.instructions || project.positioning || ''
  const isArchived = project.status === 'archived'
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'

  return (
    <div className={`rounded-lg border p-4 transition-[border-color,box-shadow] ${cardTone}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <PlatformAvatar avatarUrl={project.avatar_url} name={project.name} platform={project.platform} size="lg" />
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold text-foreground">{project.name}</h3>
            <Badge variant={platformBadge} className="mt-1 text-[10px] font-normal">
              {renderPlatformIcon(project.platform)}
              {platformLabel}
            </Badge>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {isArchived && <Badge variant="outline">已归档</Badge>}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" aria-label={`更多项目操作：${project.name}`} />}
            >
              <Ellipsis />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              {onEdit && (
                <DropdownMenuItem onClick={() => onEdit(project)}>
                  <Pencil />
                  编辑项目
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={() => setTopicPoolOpen(true)}>
                <Lightbulb />
                选题池
              </DropdownMenuItem>
              {(onArchive || onRestore) && <DropdownMenuSeparator />}
              {!isArchived && onArchive && (
                <DropdownMenuItem disabled={archiving} onClick={() => onArchive(project.id)}>
                  <Archive />
                  归档项目
                </DropdownMenuItem>
              )}
              {isArchived && onRestore && (
                <DropdownMenuItem disabled={restoring} onClick={() => onRestore(project.id)}>
                  <RotateCcw />
                  恢复项目
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{positioning}</p>
      )}
      {stats && (
        <div className="mt-4 flex min-h-8 items-center border-t border-border pt-3">
          <div className="flex gap-3 text-xs text-muted-foreground">
            <span>{stats.total_tasks} 个任务</span>
            {stats.total_tasks > 0 && <span>{stats.completed_tasks} 个已完成</span>}
          </div>
        </div>
      )}
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
    </div>
  )
}
