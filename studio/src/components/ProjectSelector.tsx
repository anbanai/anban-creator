import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { platformLabels } from '@/lib/labels'
import { Combobox, type ComboboxOption } from '@/components/Combobox'

interface ProjectSelectorProps {
  value: string
  onChange: (projectId: string, platform: string) => void
  platform?: string
}

export function ProjectSelector({ value, onChange, platform }: ProjectSelectorProps) {
  const { data: projects = [], isLoading } = useQuery({
    queryKey: ['projects', 'active', platform],
    queryFn: () => api.projects.list({ status: 'active', platform }),
  })

  const options: ComboboxOption[] = projects.map((ch) => ({
    value: ch.id,
    label: ch.name,
    group: platformLabels[ch.platform] || ch.platform,
  }))

  if (isLoading) {
    return <div className="h-8 animate-pulse rounded-lg bg-muted" />
  }

  return (
    <Combobox
      options={options}
      value={value}
      onChange={(id) => {
        if (!id) {
          onChange('', '')
          return
        }
        const ch = projects.find((c) => c.id === id)
        if (ch) onChange(ch.id, ch.platform)
      }}
      placeholder="选择项目..."
      searchPlaceholder="搜索项目..."
      emptyText="没有找到项目"
    />
  )
}
