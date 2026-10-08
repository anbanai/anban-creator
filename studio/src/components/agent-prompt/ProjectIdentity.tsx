import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { FolderKanban } from 'lucide-react'
import { contentTypeLabel, platformLabels } from '@/lib/labels'
import { SignedImage } from '@/components/ui/SignedImage'
import { AgentIconStack, agentDisplayName } from './AgentIconStack'

export interface ProjectIdentityProject {
  id: string
  name: string
  platform?: string
  avatar_url?: string
  description?: string
  positioning?: string
  instructions?: string
  keywords?: string
  agent_ids?: readonly string[]
  agent_id?: string
}

export interface ProjectIdentitySnapshot {
  project_name?: string
  platform?: string
  description?: string
  positioning?: string
  instructions?: string
  keywords?: string
  agent_ids?: readonly string[]
  agent_id?: string
}

interface ProjectIdentityProps {
  project: ProjectIdentityProject
  compact?: boolean
  showAgents?: boolean
  className?: string
}

const identityPalettes = [
  { background: 'bg-sky-100 dark:bg-sky-950', foreground: 'text-sky-700 dark:text-sky-300' },
  { background: 'bg-violet-100 dark:bg-violet-950', foreground: 'text-violet-700 dark:text-violet-300' },
  { background: 'bg-amber-100 dark:bg-amber-950', foreground: 'text-amber-700 dark:text-amber-300' },
  { background: 'bg-emerald-100 dark:bg-emerald-950', foreground: 'text-emerald-700 dark:text-emerald-300' },
  { background: 'bg-rose-100 dark:bg-rose-950', foreground: 'text-rose-700 dark:text-rose-300' },
] as const

function stableIdentityIndex(value: string) {
  let hash = 0
  for (let index = 0; index < value.length; index += 1) hash = (hash * 31 + value.charCodeAt(index)) | 0
  return (hash >>> 0) % identityPalettes.length
}

export function projectPlatformLabel(platform?: string) {
  if (!platform) return null
  return platformLabels[platform] ?? contentTypeLabel[platform] ?? '通用项目'
}

const defaultProjectAgentIDs: Record<string, readonly string[]> = {
  wechat: ['wechat-article'],
  seednote: ['seednote'],
  moments: ['moments'],
  ecommerce: ['ecommerce'],
  montage: ['montage'],
  'whiteboard-animation': ['whiteboard-animation'],
  hypit: ['hypit'],
}

export function projectAgentIDs(project: ProjectIdentityProject) {
  if (project.agent_ids?.length) return project.agent_ids
  if (project.agent_id) return [project.agent_id]
  return defaultProjectAgentIDs[project.platform ?? ''] ?? []
}

export function resolveProjectIdentity({
  project,
  snapshot,
  projectId,
  fallbackPlatform,
  fallbackName = '未设置项目',
}: {
  project?: ProjectIdentityProject
  snapshot?: ProjectIdentitySnapshot
  projectId?: string
  fallbackPlatform?: string
  fallbackName?: string
}): ProjectIdentityProject | null {
  const snapshotName = snapshot?.project_name?.trim()
  const projectName = project?.name?.trim()
  const platform = snapshot?.platform || project?.platform || fallbackPlatform
  const hasSnapshotIdentity = Boolean(
    snapshotName
    || snapshot?.platform
    || snapshot?.keywords
    || snapshot?.agent_ids?.length
    || snapshot?.agent_id,
  )
  if (!project && !hasSnapshotIdentity && !fallbackPlatform) return null
  const name = snapshotName || (hasSnapshotIdentity ? fallbackName : projectName) || fallbackName

  return {
    id: projectId || project?.id || `snapshot:${name}:${platform || ''}`,
    name,
    platform,
    avatar_url: project?.avatar_url,
    description: hasSnapshotIdentity
      ? snapshot?.description || snapshot?.positioning || snapshot?.instructions
      : project?.description || project?.positioning || project?.instructions,
    keywords: hasSnapshotIdentity ? snapshot?.keywords : project?.keywords,
    agent_ids: hasSnapshotIdentity
      ? snapshot?.agent_ids || (snapshot?.agent_id ? [snapshot.agent_id] : undefined)
      : project?.agent_ids || (project?.agent_id ? [project.agent_id] : undefined),
  }
}

export function ProjectIdentity({
  project,
  compact = false,
  showAgents = true,
  className,
}: ProjectIdentityProps) {
  const description = project.description || project.positioning || project.instructions
  const keywords = (project.keywords ?? '').split(/[,，\n]/).map((tag) => tag.trim()).filter(Boolean)
    .filter((tag, index, all) => all.indexOf(tag) === index)
  const keywordTags = keywords.slice(0, compact ? 0 : 3)
  const hiddenKeywordCount = keywords.length - keywordTags.length
  const agentIds = projectAgentIDs(project)
    .map((id) => id.trim())
    .filter((id, index, all) => id && all.indexOf(id) === index)
  const identityLabel = [
    project.name,
    showAgents ? agentIds.map(agentDisplayName).join('、') : '',
    ...keywords,
    description,
  ].filter(Boolean).join('，')
  const palette = identityPalettes[stableIdentityIndex(project.id)]
  const fallbackIcon = <FolderKanban aria-hidden="true" className={cn(compact ? 'size-3.5' : 'size-4', palette.foreground)} />
  return (
    <span
      data-slot="project-identity"
      data-compact={compact}
      aria-label={identityLabel}
      className={cn(
        'flex min-w-0 items-center text-left',
        compact ? 'h-6 gap-2' : 'min-h-10 gap-3',
        className,
      )}
    >
      <span
        data-slot="avatar"
        data-project-mark="true"
        aria-hidden="true"
        className={cn(
          'group/avatar relative flex shrink-0 select-none items-center justify-center overflow-hidden rounded-full after:absolute after:inset-0 after:rounded-full after:border after:border-border/70',
          compact ? 'size-6 text-xs' : 'size-8 text-sm',
          palette.background,
          palette.foreground,
        )}
      >
        {project.avatar_url ? (
          <SignedImage src={project.avatar_url} alt="" className="aspect-square size-full rounded-full object-cover" fallbackIcon={fallbackIcon} fallbackClassName={cn('flex size-full items-center justify-center', palette.background, palette.foreground)} showLoading={false} />
        ) : (
          fallbackIcon
        )}
      </span>
      <span className={cn('flex min-w-0 flex-1', compact ? 'items-center' : 'flex-col gap-0.5')}>
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate font-medium text-foreground">{project.name}</span>
          {showAgents ? <AgentIconStack agentIds={agentIds} compact={compact} maxVisible={compact ? 3 : 5} className="shrink-0" /> : null}
        </span>
        {!compact && keywordTags.length ? (
          <span data-slot="project-identity-keywords" aria-label="关键词" className="flex min-w-0 flex-wrap items-center gap-1 pt-0.5">
            <span className="sr-only">关键词：</span>
            {keywordTags.map((tag) => <Badge key={tag} variant="secondary" className="max-w-36 truncate">{tag}</Badge>)}
            {hiddenKeywordCount > 0 ? (
              <Badge
                variant="outline"
                title={`其余关键词：${keywords.slice(keywordTags.length).join('、')}`}
                className="shrink-0"
              >
                +{hiddenKeywordCount}
              </Badge>
            ) : null}
          </span>
        ) : null}
        {!compact && description ? (
          <span
            data-slot="project-identity-description"
            title={description}
            className="min-w-0 truncate text-xs leading-4 text-muted-foreground"
          >
            {description}
          </span>
        ) : null}
      </span>
    </span>
  )
}
