import { useState } from 'react'

import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { platformBadgeClassName, platformBgColor, platformIconColor, renderPlatformIcon } from '@/lib/PlatformIcon'
import { platformDisplayName } from '@/lib/labels'
import { cn } from '@/lib/utils'

export interface ProjectIdentityProject {
  id: string
  name: string
  platform?: string
  avatar_url?: string
  description?: string
}

interface ProjectIdentityProps {
  project: ProjectIdentityProject
  compact?: boolean
  showType?: boolean
  avatarMode?: 'project' | 'platform'
}

export function ProjectIdentity({
  project,
  compact = false,
  showType = false,
  avatarMode = 'platform',
}: ProjectIdentityProps) {
  const [failedAvatarUrl, setFailedAvatarUrl] = useState<string>()
  const initial = project.name.trim().charAt(0) || '项'
  const platformLabel = platformDisplayName(project.platform)
  const platformIcon = project.platform ? renderPlatformIcon(project.platform) : null
  const usesPlatformAvatar = avatarMode === 'platform' && Boolean(platformIcon)

  return (
    <span
      data-slot="project-identity"
      data-compact={compact}
      className={cn(
        'flex min-w-0 items-center text-left',
        compact ? 'h-6 gap-2' : 'min-h-10 gap-3',
      )}
    >
      <Avatar
        size={compact ? 'sm' : 'lg'}
        role={usesPlatformAvatar ? 'img' : undefined}
        aria-label={usesPlatformAvatar ? `${platformLabel}类型` : undefined}
        title={usesPlatformAvatar ? `${platformLabel}类型` : undefined}
        className={usesPlatformAvatar ? 'rounded-xl after:rounded-xl' : undefined}
      >
        {usesPlatformAvatar ? (
          <AvatarFallback
            className={cn(
              platformBgColor[project.platform ?? ''],
              platformIconColor[(project.platform ?? '') as keyof typeof platformIconColor],
              'rounded-xl [&_svg]:size-4',
            )}
          >
            {platformIcon}
          </AvatarFallback>
        ) : project.avatar_url && failedAvatarUrl !== project.avatar_url ? (
          <img
            src={project.avatar_url}
            alt={project.name}
            className="aspect-square size-full rounded-full object-cover"
            onError={() => setFailedAvatarUrl(project.avatar_url)}
          />
        ) : (
          <AvatarFallback
            className={cn(
              project.platform && platformBgColor[project.platform],
              project.platform && platformIconColor[project.platform as keyof typeof platformIconColor],
            )}
          >
            {initial}
          </AvatarFallback>
        )}
      </Avatar>
      <span className={cn('flex min-w-0 flex-1', compact ? 'items-center' : 'flex-col gap-0.5')}>
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate font-medium text-foreground">{project.name}</span>
          {showType ? (
            <Badge variant="secondary" className={cn('shrink-0', project.platform ? platformBadgeClassName[project.platform] : undefined)}>
              {project.platform ? renderPlatformIcon(project.platform) : null}
              {compact ? platformLabel : `${platformLabel}项目`}
            </Badge>
          ) : null}
        </span>
        {!compact && project.description ? (
          <span
            data-slot="project-identity-description"
            title={project.description}
            className="min-w-0 truncate text-xs leading-4 text-muted-foreground"
          >
            {project.description}
          </span>
        ) : null}
      </span>
    </span>
  )
}
