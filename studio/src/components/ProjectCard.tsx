import { useState } from 'react'
import { Link } from 'react-router-dom'
import { portraitProgress, portraitStatus, portraitSummary } from '@/lib/portrait-library'
import { Archive, Brain, GalleryHorizontalEnd, Lightbulb, RotateCcw, Settings, SquarePen, ScanFace } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { Button, buttonVariants } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ProjectMemoryDialog } from '@/components/projects/ProjectMemoryDialog'
import { ImageAnalysisBadge } from '@/components/image-analysis/ImageAnalysisBadge'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { StatusPill } from '@/components/workspace'
import { supportsProfileLab } from '@/lib/project-capabilities'
import { cn } from '@/lib/utils'

const actionButtonClass = 'shrink-0'

function profileStatusLabel(project: Project) {
  return project.profile?.initialization_status === 'ready' ? `画像已应用 · v${project.profile.version}` : portraitStatus(project.profile)
}

interface ProjectCardProps {
  project: Project
  stats?: ProjectStats
  onEdit?: (project: Project) => void
  onChannelConfig?: (project: Project) => void
  onUpdateProfile?: (project: Project) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
}

export function ProjectCard({ project, stats, onEdit, onChannelConfig, onUpdateProfile, archiving, restoring, onArchive, onRestore }: ProjectCardProps) {
  const [memoryOpen, setMemoryOpen] = useState(false)
  const isArchived = project.status === 'archived'
  const unusedTopics = stats?.unused_topics
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'
  const editLabel = '编辑人设'
  const profileProgress = portraitProgress(project.profile)
  const profileSummary = portraitSummary(project)

  return (
    <div className={`relative isolate flex h-full flex-col rounded-lg border p-4 transition-[border-color,box-shadow] [&_button]:relative [&_button]:z-10 [&_a]:relative [&_a]:z-10 ${cardTone}`}>
      <Link to={`/portraits/${project.id}`} aria-label={`查看人设详情：${project.name}`} className="!absolute inset-0 !z-0 rounded-lg focus-visible:outline-2 focus-visible:outline-primary" />
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <h3 className="min-w-0 flex-1">
            <ProjectIdentity project={project} showCapabilityLabels />
          </h3>
        </div>
        <div className="flex flex-col items-end gap-1">
          {isArchived && <StatusPill status="archived" label="已归档" />}
          <ImageAnalysisBadge analysis={project.image_analysis} />
        </div>
      </div>
      <div className="flex flex-1 flex-col pt-4">
        {supportsProfileLab(project) && (
          <section className="mb-3 border-t border-border pt-3" aria-label={`人设画像：${project.name}`}>
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">画像</p>
                  <span data-profile-status={project.profile?.initialization_status ?? 'not_started'} className="text-xs text-muted-foreground">
                    {profileStatusLabel(project)}
                  </span>
                </div>
                <p className="mt-1 h-10 line-clamp-2 text-sm leading-5 text-foreground">{profileSummary}</p>
              </div>
              <span className="shrink-0 text-xs font-medium text-muted-foreground">{profileProgress.count}/6 项</span>
            </div>
            <div className="mt-3 mb-1 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label={`${project.name}画像完善度`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={profileProgress.percent}>
              <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${profileProgress.percent}%` }} />
            </div>
          </section>
        )}
        <div className="mt-auto flex flex-nowrap justify-end gap-0" aria-label={`人设操作：${project.name}`}>
          {supportsProfileLab(project) && (
            <Tooltip>
              <TooltipTrigger render={<Link to={`/portraits/${project.id}`} className={cn(buttonVariants({ variant: 'ghost', size: 'icon-md' }), actionButtonClass)} aria-label={`查看画像：${project.name}`} />}>
                <ScanFace />
              </TooltipTrigger>
              <TooltipContent>查看画像</TooltipContent>
            </Tooltip>
          )}
          {onEdit && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" className={actionButtonClass} onClick={() => onEdit(project)} aria-label={`${editLabel}：${project.name}`} />}>
                <SquarePen />
              </TooltipTrigger>
              <TooltipContent>编辑</TooltipContent>
            </Tooltip>
          )}
          {onChannelConfig && (project.platform === 'wechat' || project.agent_ids?.some((id) => id === 'wechat-article' || id === 'wechat-picture')) && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" className={actionButtonClass} onClick={() => onChannelConfig(project)} aria-label={`平台设置：${project.name}`} />}>
                <Settings />
              </TooltipTrigger>
              <TooltipContent>平台设置</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Link to={`/topics?project_id=${encodeURIComponent(project.id)}`} className={cn(buttonVariants({ variant: 'ghost', size: 'icon-md' }), actionButtonClass)} aria-label={unusedTopics === undefined ? `选题库：${project.name}` : `选题库：${project.name}，未使用 ${unusedTopics} 个`} />}>
              <Lightbulb />
            </TooltipTrigger>
            <TooltipContent>{unusedTopics === undefined ? '选题库' : `选题库：未使用 ${unusedTopics} 个`}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon-md" className={actionButtonClass} onClick={() => setMemoryOpen(true)} aria-label={`人设记忆：${project.name}`} />}>
              <Brain />
            </TooltipTrigger>
              <TooltipContent>人设记忆</TooltipContent>
          </Tooltip>
          {!isArchived && onArchive && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" className={actionButtonClass} disabled={archiving} onClick={() => onArchive(project.id)} aria-label={`归档人设：${project.name}`} />}>
                <Archive />
              </TooltipTrigger>
              <TooltipContent>归档</TooltipContent>
            </Tooltip>
          )}
          {isArchived && onRestore && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" className={actionButtonClass} disabled={restoring} onClick={() => onRestore(project.id)} aria-label={`取消归档人设：${project.name}`} />}>
                <RotateCcw />
              </TooltipTrigger>
              <TooltipContent>取消归档</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Link to={`/tasks?project_id=${encodeURIComponent(project.id)}`} className={cn(buttonVariants({ variant: 'ghost', size: 'icon-md' }), actionButtonClass)} aria-label={`查看作品：${project.name}`} />}>
              <GalleryHorizontalEnd />
            </TooltipTrigger>
            <TooltipContent>查看作品</TooltipContent>
          </Tooltip>
        </div>
      </div>
      <ProjectMemoryDialog projectId={project.id} projectName={project.name} project={project} open={memoryOpen} onOpenChange={setMemoryOpen} onUpdateProfile={onUpdateProfile ? () => onUpdateProfile(project) : undefined} />
    </div>
  )
}
