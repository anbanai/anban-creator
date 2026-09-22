import { useState } from 'react'
import { Archive, Brain, Lightbulb, Pencil, RotateCcw } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { platformLabels } from '@/lib/labels'
import { renderPlatformIcon, platformBadgeVariant } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import { ProjectMemoryDialog } from '@/components/projects/ProjectMemoryDialog'
import { ImageAnalysisBadge } from '@/components/image-analysis/ImageAnalysisBadge'

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
  const [memoryOpen, setMemoryOpen] = useState(false)
  const platformLabel = platformLabels[project.platform] || project.platform
  const platformBadge = platformBadgeVariant[project.platform] || ('secondary' as const)
  const positioning = project.instructions || project.positioning || ''
  const isArchived = project.status === 'archived'
  const unusedTopics = stats?.unused_topics
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'

  return (
    <div className={`flex h-full flex-col rounded-lg border p-4 transition-[border-color,box-shadow] ${cardTone}`}>
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
        <div className="flex flex-col items-end gap-1">
          {isArchived && <Badge variant="outline">已归档</Badge>}
          <ImageAnalysisBadge analysis={project.image_analysis} />
        </div>
      </div>
      {positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{positioning}</p>
      )}
      <div className="mt-auto pt-4">
        {stats && (
          <div className="flex min-h-8 items-center border-t border-border pt-3">
            <div className="flex gap-3 text-xs text-muted-foreground">
              <span>{stats.total_tasks} 个任务</span>
              {stats.total_tasks > 0 && <span>{stats.completed_tasks} 个已完成</span>}
            </div>
          </div>
        )}
        <div className="mt-2 flex flex-wrap justify-end gap-1">
          {onEdit && (
            <Button variant="ghost" size="xs" onClick={() => onEdit(project)} aria-label={`编辑项目：${project.name}`}>
              <Pencil />
              编辑
            </Button>
          )}
          <Button variant="ghost" size="xs" onClick={() => setMemoryOpen(true)} aria-label={`项目记忆：${project.name}`}>
            <Brain />
            记忆
          </Button>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => setTopicPoolOpen(true)}
            aria-label={unusedTopics === undefined
              ? `选题池：${project.name}`
              : `选题池：${project.name}，剩余 ${unusedTopics} 个`}
          >
            <Lightbulb />
            选题池
            {unusedTopics !== undefined && (
              <span className="min-w-4 text-center tabular-nums text-foreground">{unusedTopics}</span>
            )}
          </Button>
          {!isArchived && onArchive && (
            <Button variant="ghost" size="xs" disabled={archiving} onClick={() => onArchive(project.id)} aria-label={`归档项目：${project.name}`}>
              <Archive />
              归档
            </Button>
          )}
          {isArchived && onRestore && (
            <Button variant="ghost" size="xs" disabled={restoring} onClick={() => onRestore(project.id)} aria-label={`恢复项目：${project.name}`}>
              <RotateCcw />
              恢复
            </Button>
          )}
        </div>
      </div>
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
      <ProjectMemoryDialog projectId={project.id} projectName={project.name} open={memoryOpen} onOpenChange={setMemoryOpen} />
    </div>
  )
}
