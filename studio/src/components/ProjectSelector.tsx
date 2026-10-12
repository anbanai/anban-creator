import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'

interface ProjectSelectorProps {
  value: string
  onChange: (projectId: string, platform: string) => void
  platform?: string
  excludePlatforms?: string[]
  disabled?: boolean
  includeArchived?: boolean
}

export function ProjectSelector({ value, onChange, platform, excludePlatforms = [], disabled = false, includeArchived = false }: ProjectSelectorProps) {
  const { data: projects = [], isLoading } = useQuery({
    queryKey: ['projects', includeArchived ? 'all' : 'active', platform],
    queryFn: () => api.projects.list({ status: includeArchived ? undefined : 'active', platform }),
  })

  const visibleProjects = projects.filter((ch) => !excludePlatforms.includes(ch.platform ?? ''))

  return (
    <ProjectContextControl
      mode="select"
      projects={visibleProjects}
      value={value || null}
      allowNoProject
      noProjectLabel="全部创作项目"
      compact
      ariaLabel="筛选创作项目"
      loading={isLoading}
      disabled={disabled}
      onValueChange={(id, project) => {
        if (!id || !project) {
          onChange('', '')
          return
        }
        onChange(id, project.platform ?? '')
      }}
    />
  )
}
