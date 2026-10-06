import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { contentTypeLabel, platformLabels } from '@/lib/labels'
import { platformIconColor, renderPlatformIcon } from '@/lib/PlatformIcon'
import { SignedImage } from '@/components/ui/SignedImage'

export interface ProjectIdentityProject {
  id: string
  name: string
  platform?: string
  avatar_url?: string
  description?: string
  positioning?: string
  instructions?: string
  keywords?: string
}

export interface ProjectIdentitySnapshot {
  project_name?: string
  platform?: string
  description?: string
  positioning?: string
  instructions?: string
  keywords?: string
}

interface ProjectIdentityProps {
  project: ProjectIdentityProject
  compact?: boolean
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
  const hasSnapshotIdentity = Boolean(snapshotName || snapshot?.platform || snapshot?.keywords)
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
  }
}

export function ProjectIdentity({
  project,
  compact = false,
  className,
}: ProjectIdentityProps) {
  const description = project.description || project.positioning || project.instructions
  const platformLabel = projectPlatformLabel(project.platform)
  const allTags = [
    platformLabel,
    ...(project.keywords ?? '').split(/[,，\n]/).map((tag) => tag.trim()).filter(Boolean),
  ].filter((tag, index, all) => all.indexOf(tag) === index)
  const tags = allTags.slice(0, compact ? 1 : 3)
  const hiddenTagCount = allTags.length - tags.length
  const initials = project.name.trim().split(/\s+/).map((part) => part[0]).join('').slice(0, 2).toUpperCase() || '项目'
  const identityLabel = [project.name, ...allTags, description].filter(Boolean).join('，')
  const palette = identityPalettes[stableIdentityIndex(`${project.id}:${project.platform ?? ''}`)]
  const platformIcon = project.platform ? renderPlatformIcon(project.platform) : null
  const fallbackIcon = platformIcon
    ? <span className={cn('flex size-full items-center justify-center', project.platform ? platformIconColor[project.platform] : undefined)}>{platformIcon}</span>
    : <span className="font-semibold">{initials}</span>
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
          {compact && tags[0] ? <Badge variant="secondary" className="max-w-28 truncate">{tags[0]}</Badge> : null}
          {compact && hiddenTagCount > 0 ? (
            <Badge
              variant="outline"
              title={`其余标签：${allTags.slice(tags.length).join('、')}`}
              className="shrink-0"
            >
              +{hiddenTagCount}
            </Badge>
          ) : null}
        </span>
        {!compact && tags.length ? (
          <span data-slot="project-identity-tags" className="flex min-w-0 flex-wrap gap-1 pt-0.5">
            {tags.map((tag) => <Badge key={tag} variant="secondary" className="max-w-36 truncate">{tag}</Badge>)}
            {hiddenTagCount > 0 ? (
              <Badge
                variant="outline"
                title={`其余标签：${allTags.slice(tags.length).join('、')}`}
                className="shrink-0"
              >
                +{hiddenTagCount}
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
