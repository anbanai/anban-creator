import type { ReactNode } from 'react'
import { BriefcaseBusiness, FileText, LockKeyhole, PanelsTopLeft, UserRound } from 'lucide-react'
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

function modeLabel(mode: AssistantModeID, modes: readonly AssistantMode[]) {
  if (mode === 'profile') return '人设'
  return modes.find((candidate) => candidate.id === mode)?.displayName
    ?? (mode === 'creation' ? '创作' : '复盘')
}

function modeIcon(mode: AssistantModeID) {
  if (mode === 'profile') return UserRound
  if (mode === 'creation') return PanelsTopLeft
  return FileText
}

function ContextLabel({ icon: Icon, children }: { icon: typeof UserRound; children: ReactNode }) {
  return <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground"><Icon aria-hidden="true" className="size-3.5 shrink-0" />{children}</span>
}

export function AssistantContextBar(props: AssistantContextBarProps) {
  const {
    mode,
    modes,
    projects,
    projectId,
    works,
    contentId,
    creationType,
    creationTypeOptions,
    loadingProjects = false,
    loadingWorks = false,
    hasMoreWorks = false,
  } = props
  const selectedProject = projects.find((project) => project.id === projectId) ?? null
  const selectedWork = works.find((work) => work.id === contentId) ?? null
  const selectedCreationType = creationTypeOptions.find((option) => option.value === creationType)
  const ModeIcon = modeIcon(mode)

  if (props.phase === 'locked') {
    const projectFallback = loadingProjects
      ? '加载人设…'
      : mode === 'profile' && !projectId
        ? '新人设'
        : projectId ? '已选人设' : '未关联人设'
    return (
      <div data-testid="assistant-context-bar" data-phase="locked" aria-disabled="true" className="flex w-full flex-wrap items-center gap-x-4 gap-y-2 border-b border-border/50 py-3 text-left text-sm text-muted-foreground">
        <ContextLabel icon={UserRound}><ProjectContextControl mode="readonly" project={selectedProject} noProjectLabel={projectFallback} compact showAgents={false} /></ContextLabel>
        <ContextLabel icon={ModeIcon}>{modeLabel(mode, modes)}</ContextLabel>
        {mode === 'creation' && (selectedCreationType || creationType) ? <ContextLabel icon={PanelsTopLeft}>{selectedCreationType?.label ?? creationType}</ContextLabel> : null}
        {mode === 'review' && (contentId || selectedWork) ? <ContextLabel icon={FileText}>{selectedWork?.title ?? (loadingWorks ? '加载作品…' : '已选作品')}</ContextLabel> : null}
        <LockKeyhole aria-label="会话上下文已锁定" className="ml-auto size-3.5 shrink-0 text-muted-foreground/70" />
      </div>
    )
  }

  const modeOptions = modes.length ? modes : MODES_FALLBACK
  const projectValue = projectId || null
  const disabled = props.disabled ?? false
  return (
    <div data-testid="assistant-context-bar" data-phase="setup" className="flex w-full flex-wrap items-center gap-2 border-b border-border/60 py-3 text-left">
      <span className="sr-only">会话上下文</span>
      {(projects.length > 0 || mode === 'profile') ? <ContextLabel icon={UserRound}>
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
      </ContextLabel> : null}
      <ContextLabel icon={ModeIcon}>
        <Select value={mode} disabled={disabled || loadingProjects} onValueChange={(value) => { if (value) props.onModeChange(value as AssistantModeID) }}>
          <SelectTrigger size="sm" aria-label="助手模式" className="h-8 border-0 bg-transparent px-1.5 font-medium shadow-none hover:bg-muted/60">
            <SelectValue>{modeLabel(mode, modes)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {modeOptions.map((candidate) => {
              const modeStatus = candidate.available ? '可用' : '暂不可用'
              const modeDescription = candidate.available ? candidate.description : candidate.disabledReason ?? candidate.description
              return (
                <SelectItem
                  key={candidate.id}
                  value={candidate.id}
                  label={candidate.displayName}
                  disabled={candidate.disabledReason === '助手运行环境暂不可用，请稍后重试'}
                  className="py-2"
                >
                  <span className="flex min-w-0 flex-col gap-0.5 whitespace-normal">
                    <span className="font-medium">{candidate.displayName}</span>
                    <span className="text-xs text-muted-foreground">{modeStatus}{modeDescription ? ` · ${modeDescription}` : ''}</span>
                  </span>
                </SelectItem>
              )
            })}
          </SelectContent>
        </Select>
      </ContextLabel>
      {mode === 'creation' && projectId && creationTypeOptions.length > 0 ? (
        <ContextLabel icon={PanelsTopLeft}>
          <Select value={creationType} disabled={disabled} onValueChange={(value) => { if (value) props.onCreationTypeChange(value) }}>
            <SelectTrigger size="sm" aria-label="创作类型" className="h-8 border-0 bg-transparent px-1.5 shadow-none hover:bg-muted/60"><SelectValue>{selectedCreationType?.label ?? '选择创作类型'}</SelectValue></SelectTrigger>
            <SelectContent>{creationTypeOptions.map((option) => <SelectItem key={option.value} value={option.value} label={option.label}>{option.label}</SelectItem>)}</SelectContent>
          </Select>
        </ContextLabel>
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

const MODES_FALLBACK: AssistantMode[] = [
  { id: 'profile', displayName: '画像模式', description: '', available: true },
  { id: 'creation', displayName: '创作模式', description: '', available: true },
  { id: 'review', displayName: '复盘模式', description: '', available: true },
]
