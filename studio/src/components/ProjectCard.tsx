import { useState } from 'react'
import { Link } from 'react-router-dom'
import { accountPlatformLabels, portraitProgress, portraitStatus, portraitSummary, updatePortraitHref } from '@/lib/portrait-library'
import { Archive, Settings2, Brain, Lightbulb, ListChecks, RotateCcw, SquarePen } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { Button, buttonVariants } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import { ProjectMemoryDialog } from '@/components/projects/ProjectMemoryDialog'
import { ImageAnalysisBadge } from '@/components/image-analysis/ImageAnalysisBadge'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { StatusPill } from '@/components/workspace'
import { supportsProfileLab } from '@/lib/project-capabilities'

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

export function ProjectCard({ project, stats, onEdit, onUpdateProfile, onChannelConfig, archiving, restoring, onArchive, onRestore }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const isArchived = project.status === 'archived'
  const unusedTopics = stats?.unused_topics
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'
  const editLabel = '编辑项目'
  const profileProgress = portraitProgress(project.profile)
  const profileSummary = portraitSummary(project)
  const profileAccounts = (project.profile_setup?.platform_accounts ?? []).map((account) => `${accountPlatformLabels[account.platform] ?? account.platform}${account.account_name ? ` · ${account.account_name}` : ''}`)

  return (
    <div className={`flex h-full flex-col rounded-lg border p-4 transition-[border-color,box-shadow] ${cardTone}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <h3 className="min-w-0 flex-1">
            <ProjectIdentity project={project} />
          </h3>
        </div>
        <div className="flex flex-col items-end gap-1">
          {isArchived && <StatusPill status="archived" label="已归档" />}
          <ImageAnalysisBadge analysis={project.image_analysis} />
        </div>
      </div>
      <div className="pt-4">
        {supportsProfileLab(project) && (
          <section className="mb-3 border-y border-border py-3" aria-label={`人设画像：${project.name}`}>
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">画像</p>
                  <span data-profile-status={project.profile?.initialization_status ?? 'not_started'} className="text-xs text-muted-foreground">
                    {profileStatusLabel(project)}
                  </span>
                </div>
                <p className="mt-1 line-clamp-2 text-sm leading-5 text-foreground">{profileSummary}</p>
              </div>
              <span className="shrink-0 text-xs font-medium text-muted-foreground">{profileProgress.count}/6 项</span>
            </div>
            <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label={`${project.name}画像完善度`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={profileProgress.percent}>
              <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${profileProgress.percent}%` }} />
            </div>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="min-w-0 truncate text-muted-foreground">{profileAccounts.length ? profileAccounts.join(' / ') : '尚未添加平台账号'}</span>
              <div className="flex shrink-0 items-center gap-3">
                <Link to={`/portraits/${project.id}`} className="font-medium text-primary hover:underline">查看画像</Link>
                {onUpdateProfile && <Link to={updatePortraitHref(project.id)} className="text-muted-foreground hover:text-foreground">继续访谈</Link>}
              </div>
            </div>
          </section>
        )}
        <div className="mt-2 flex flex-wrap justify-end gap-1" aria-label={`项目操作：${project.name}`}>
          {onEdit && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => onEdit(project)} aria-label={`${editLabel}：${project.name}`} />}>
                <SquarePen />
              </TooltipTrigger>
              <TooltipContent>编辑</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => setTopicPoolOpen(true)} aria-label={unusedTopics === undefined ? `选题资产：${project.name}` : `选题资产：${project.name}，待研究 ${unusedTopics} 个`} />}>
              <Lightbulb />
            </TooltipTrigger>
            <TooltipContent>{unusedTopics === undefined ? '选题资产' : `选题资产：待研究 ${unusedTopics} 个`}</TooltipContent>
          </Tooltip>
          {onChannelConfig && (project.platform === 'wechat' || project.agent_ids?.some((id) => id === 'wechat-article' || id === 'wechat-picture')) && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => onChannelConfig(project)} aria-label={`平台设置：${project.name}`} />}>
                <Settings2 />
              </TooltipTrigger>
              <TooltipContent>平台设置</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => setMemoryOpen(true)} aria-label={`项目记忆：${project.name}`} />}>
              <Brain />
            </TooltipTrigger>
              <TooltipContent>项目记忆</TooltipContent>
          </Tooltip>
          {!isArchived && onArchive && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={archiving} onClick={() => onArchive(project.id)} aria-label={`归档项目：${project.name}`} />}>
                <Archive />
              </TooltipTrigger>
              <TooltipContent>归档</TooltipContent>
            </Tooltip>
          )}
          {isArchived && onRestore && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={restoring} onClick={() => onRestore(project.id)} aria-label={`恢复项目：${project.name}`} />}>
                <RotateCcw />
              </TooltipTrigger>
              <TooltipContent>恢复</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Link to={`/tasks?project_id=${encodeURIComponent(project.id)}`} className={buttonVariants({ variant: 'ghost', size: 'icon-md' })} aria-label={`查看作品：${project.name}`} />}>
              <ListChecks />
            </TooltipTrigger>
            <TooltipContent>查看作品</TooltipContent>
          </Tooltip>
        </div>
      </div>
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
      <ProjectMemoryDialog projectId={project.id} projectName={project.name} project={project} open={memoryOpen} onOpenChange={setMemoryOpen} onUpdateProfile={onUpdateProfile ? () => onUpdateProfile(project) : undefined} />
    </div>
  )
}
