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
}

export function ProjectIdentity({
  project,
  compact = false,
}: ProjectIdentityProps) {
  return (
    <span
      data-slot="project-identity"
      data-compact={compact}
      className={cn(
        'flex min-w-0 items-center text-left',
        compact ? 'h-6 gap-2' : 'min-h-10 gap-3',
      )}
    >
      <span className={cn('flex min-w-0 flex-1', compact ? 'items-center' : 'flex-col gap-0.5')}>
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate font-medium text-foreground">{project.name}</span>
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
