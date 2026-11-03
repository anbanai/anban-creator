import { ArrowUpRight } from 'lucide-react'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { Button } from '@/components/ui/button'
import { contentTypeLabel } from '@/lib/labels'
import type { Project, ProjectProfile, Task } from '@/types'
import type { TaskDetailsTab } from './TaskDetailsSheet'

interface TaskCreationSidebarProps {
  task: Task
  project?: Project
  profile: ProjectProfile | null
  onOpenPersona: () => void
  onOpenTab: (tab: TaskDetailsTab) => void
}

function profileSummary(content?: Record<string, unknown>) {
  for (const key of ['summary', 'description', 'positioning', 'tone', 'audience', 'content']) {
    const value = content?.[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function imageComposition(task: Task) {
  if (task.type === 'wechat-article') {
    const images = [task.article_with_cover && '封面', task.article_with_content_images && '正文配图'].filter(Boolean)
    if (images.length) return images.join(' + ')
    if (task.article_with_cover === false && task.article_with_content_images === false) return '不生成配图'
  }
  if (task.type === 'seednote' && (task.has_content_image !== undefined || task.has_tail_image !== undefined)) {
    return ['封面', task.has_content_image && '内容图', task.has_tail_image && '尾图'].filter(Boolean).join(' + ')
  }
  return ''
}

export function TaskCreationSidebar({ task, project, profile, onOpenPersona, onOpenTab }: TaskCreationSidebarProps) {
  const snapshot = task.project_snapshot
  const frozenName = snapshot?.project_name?.trim()
  const positioning = project ? profileSummary(profile?.dimensions.identity.content)
    || project.description?.trim() || project.positioning?.trim() || project.instructions?.trim() : ''
  const audience = project ? profileSummary(profile?.dimensions.audience.content) : ''
  const style = project ? profileSummary(profile?.dimensions.style.content) : ''
  const personaFacts = [
    { label: '定位', value: positioning },
    audience ? { label: '受众', value: audience } : { label: '风格', value: style },
  ].filter((fact) => fact.value)
  const contentType = contentTypeLabel[task.channel || snapshot?.channel || task.type]
  const ratio = task.image_ratio || (snapshot ? snapshot.image_ratio : project?.image_ratio)
  const composition = imageComposition(task)
  const attachmentCount = task.input_attachments?.length ?? 0
  const hasCreation = Boolean(contentType || ratio || composition || attachmentCount)
  const hasPersona = Boolean(project || frozenName)
  const completed = task.status === 'completed'

  if (!hasPersona && !hasCreation && !completed) return null

  return (
    <section aria-label="创作参考" className="divide-y divide-border/70 rounded-xl border border-border/70 bg-muted/10 text-sm">
      {hasPersona && (
        <div className="space-y-3 p-4">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-xs font-medium text-muted-foreground">{project ? '当前人设' : '创建时人设'}</h2>
            {project && (
              <Button type="button" variant="ghost" size="icon-sm" aria-label="打开完整人设" onClick={onOpenPersona}>
                <ArrowUpRight aria-hidden="true" className="size-3.5" />
              </Button>
            )}
          </div>
          {project ? <ProjectIdentity project={project} compact showAgents={false} /> : <p className="truncate font-medium" title={frozenName}>{frozenName}</p>}
          {personaFacts.length > 0 && (
            <dl className="space-y-2 text-xs leading-5">
              {personaFacts.map((fact) => (
                <div key={fact.label} className="grid grid-cols-[2rem_minmax(0,1fr)] gap-2">
                  <dt className="text-muted-foreground">{fact.label}</dt>
                  <dd className="line-clamp-2 break-words text-foreground" title={fact.value}>{fact.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      )}
      {hasCreation && (
        <div className="space-y-3 p-4">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-xs font-medium text-muted-foreground">本次创作</h2>
            <Button type="button" variant="ghost" size="icon-sm" aria-label="打开创作配置" onClick={() => onOpenTab('configuration')}>
              <ArrowUpRight aria-hidden="true" className="size-3.5" />
            </Button>
          </div>
          <div className="space-y-1.5">
            {contentType && <p className="font-medium">{contentType}</p>}
            {(composition || ratio) && <p className="text-xs leading-5 text-muted-foreground">{[composition, ratio && `画幅 ${ratio}`].filter(Boolean).join(' · ')}</p>}
            {attachmentCount > 0 && (
              <Button type="button" variant="link" className="h-auto p-0 text-xs text-muted-foreground" aria-label="打开参考素材" onClick={() => onOpenTab('materials')}>
                参考素材 {attachmentCount} 项
              </Button>
            )}
          </div>
        </div>
      )}
      {completed && <p className="px-4 py-3 text-xs text-muted-foreground">总消耗 {(task.billing_total_credits ?? task.billing_price_credits).toLocaleString()} 积分</p>}
    </section>
  )
}
