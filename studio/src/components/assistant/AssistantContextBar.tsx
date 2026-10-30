import type { ReactNode } from 'react'
import { BriefcaseBusiness, FileText, UserRound } from 'lucide-react'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import type { AssistantMode, AssistantModeID, AssistantProject, AssistantWork } from '@/lib/api/assistant'

export type AssistantContextBarPhase = 'setup' | 'locked'

const LOAD_MORE_WORKS_VALUE = '__assistant_load_more_works__'

interface AssistantContextBarCommonProps {
  mode: AssistantModeID
  modes: readonly AssistantMode[]
  projects: readonly AssistantProject[]
  projectId: string
  works: readonly AssistantWork[]
  contentId: string
  creationType: string
  creationTypeOptions: readonly { value: string; label: string }[]
  loadingProjects?: boolean
  loadingWorks?: boolean
  hasMoreWorks?: boolean
  status?: ReactNode
}
export type AssistantContextBarProps = AssistantContextBarCommonProps & (
  | {
      phase: 'setup'
      disabled?: boolean
      onModeChange: (mode: AssistantModeID) => void
      onProjectChange: (projectId: string | null) => void
      onContentChange: (contentId: string) => void
      onCreationTypeChange: (value: string) => void
      onLoadMoreWorks?: () => void
    }
  | {
      phase: 'locked'
      onModeChange?: never
      onProjectChange?: never
      onContentChange?: never
      onCreationTypeChange?: never
      onLoadMoreWorks?: never
    }
)

function ContextLabel({ icon: Icon, children }: { icon: typeof UserRound; children: ReactNode }) {
  return <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground"><Icon aria-hidden="true" className="size-3.5 shrink-0" />{children}</span>
}

function modeLabel(mode: AssistantModeID) {
  return mode === 'profile' ? '人设模式' : mode === 'creation' ? '创作模式' : '复盘模式'
}

export function AssistantContextBar(props: AssistantContextBarProps) {
  const {
    mode,
    projects,
    projectId,
    works,
    contentId,
    loadingProjects = false,
    loadingWorks = false,
    hasMoreWorks = false,
    status,
  } = props
  const selectedProject = projects.find((project) => project.id === projectId) ?? null
  const selectedWork = works.find((work) => work.id === contentId) ?? null
  if (props.phase === 'locked') {
    const projectFallback = loadingProjects
      ? '加载人设…'
      : mode === 'profile' && !projectId
        ? '新人设'
        : projectId ? '已选人设' : '未关联人设'
    return (
      <div data-testid="assistant-context-bar" data-phase="locked" aria-disabled="true" className="flex w-full flex-wrap items-center gap-x-4 gap-y-2 py-3 text-left text-sm text-muted-foreground">
        <ProjectContextControl mode="readonly" project={selectedProject} noProjectLabel={projectFallback} compact showAgents={false} />
        {status}
        {mode === 'review' && (contentId || selectedWork) ? <ContextLabel icon={FileText}>{selectedWork?.title ?? (loadingWorks ? '加载作品…' : '已选作品')}</ContextLabel> : null}
      </div>
    )
  }

  const projectValue = projectId || null
  const disabled = props.disabled ?? false
  return (
    <div data-testid="assistant-context-bar" data-phase="setup" className="flex w-full flex-wrap items-center gap-2 py-3 text-left">
      <span className="sr-only">会话上下文</span>
      <ContextLabel icon={BriefcaseBusiness}>当前模式：{modeLabel(mode)}</ContextLabel>
      {(projects.length > 0 || mode === 'profile') ? (
        <ProjectContextControl
          mode="select"
          compact
          projects={projects}
          value={projectValue}
          allowNoProject={mode === 'profile'}
          noProjectLabel="新人设"
          placeholder={mode === 'profile' ? '新人设' : '选择人设'}
          ariaLabel="选择画像"
          loading={loadingProjects}
          disabled={disabled}
          showAgents={false}
          onValueChange={(value) => props.onProjectChange(value)}
        />
      ) : null}
      {mode === 'review' && projectId ? (
        <ContextLabel icon={FileText}>
          <Select value={contentId} disabled={disabled} onValueChange={(value) => {
            if (!value) return
            if (value === LOAD_MORE_WORKS_VALUE) {
              props.onLoadMoreWorks?.()
              return
            }
            props.onContentChange(value)
          }}>
            <SelectTrigger size="sm" aria-label="选择作品" className="h-8 max-w-64 border-0 bg-transparent px-1.5 shadow-none hover:bg-muted/60"><SelectValue>{selectedWork?.title ?? (loadingWorks ? '加载作品…' : '选择作品')}</SelectValue></SelectTrigger>
            <SelectContent>
              {works.map((work) => <SelectItem key={work.id} value={work.id} label={work.title}>{work.title}</SelectItem>)}
              {hasMoreWorks && props.onLoadMoreWorks ? <SelectItem value={LOAD_MORE_WORKS_VALUE} label="加载更多作品" disabled={loadingWorks} className="py-2"><span className="inline-flex items-center gap-2 text-xs text-muted-foreground"><BriefcaseBusiness className="size-3.5" />加载更多作品</span></SelectItem> : null}
            </SelectContent>
          </Select>
        </ContextLabel>
      ) : null}
    </div>
  )
}
