import { Link } from 'react-router-dom'
import { LoaderCircleIcon, PlusIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxCollection,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxLabel,
  ComboboxList,
  ComboboxTrigger,
} from '@/components/ui/combobox'
import { Separator } from '@/components/ui/separator'
import { platformLabels } from '@/lib/labels'

export interface ProjectContextProject {
  id: string
  name: string
  platform?: string
}

interface ProjectContextSelectProps {
  mode: 'select'
  projects: readonly ProjectContextProject[]
  value: string | null
  onValueChange: (id: string | null, project?: ProjectContextProject) => void
  allowNoProject?: boolean
  createProjectHref?: string
  loading?: boolean
  disabled?: boolean
  placeholder?: string
}

interface ProjectContextReadonlyProps {
  mode: 'readonly'
  project: ProjectContextProject | null
  noProjectLabel?: string
}

interface ProjectContextHiddenProps {
  mode: 'hidden'
}

export type ProjectContextControlProps =
  | ProjectContextSelectProps
  | ProjectContextReadonlyProps
  | ProjectContextHiddenProps

interface ProjectItem extends ProjectContextProject {
  kind: 'project'
}

interface NoProjectItem {
  kind: 'none'
  id: '__no-project__'
  name: '不使用项目'
}

type ProjectContextItem = ProjectItem | NoProjectItem

interface ProjectContextGroup {
  value: string
  items: ProjectContextItem[]
}

const NO_PROJECT_ITEM: NoProjectItem = {
  kind: 'none',
  id: '__no-project__',
  name: '不使用项目',
}

function projectGroups(
  projects: readonly ProjectContextProject[],
  allowNoProject: boolean,
): ProjectContextGroup[] {
  const groups = new Map<string, ProjectItem[]>()
  for (const project of projects) {
    const label = project.platform ? (platformLabels[project.platform] ?? project.platform) : '项目'
    const group = groups.get(label) ?? []
    group.push({ ...project, kind: 'project' })
    groups.set(label, group)
  }

  const result: ProjectContextGroup[] = []
  if (allowNoProject) result.push({ value: '上下文', items: [NO_PROJECT_ITEM] })
  for (const [value, items] of groups) result.push({ value, items })
  return result
}

function SelectProjectContext({
  projects,
  value,
  onValueChange,
  allowNoProject = false,
  createProjectHref,
  loading = false,
  disabled = false,
  placeholder = '选择项目...',
}: ProjectContextSelectProps) {
  const groups = projectGroups(projects, allowNoProject)
  const projectItems = groups.flatMap((group) => group.items)
  const selected = value === null
    ? (allowNoProject ? NO_PROJECT_ITEM : null)
    : projectItems.find((item) => item.kind === 'project' && item.id === value) ?? null
  const isDisabled = loading || disabled

  return (
    <div data-slot="project-context-control" data-mode="select" className="min-w-0">
      <Combobox
        items={groups}
        value={selected}
        disabled={isDisabled}
        itemToStringValue={(item: ProjectContextItem) => item.name}
        onValueChange={(item: ProjectContextItem | null) => {
          if (!item || item.kind === 'none') {
            onValueChange(null, undefined)
            return
          }
          const project = projects.find((candidate) => candidate.id === item.id)
          onValueChange(item.id, project)
        }}
      >
        <ComboboxTrigger
          render={
            <Button
              type="button"
              variant="outline"
              aria-label="项目上下文"
              disabled={isDisabled}
              className="w-full min-w-0 justify-between font-normal"
            />
          }
        >
          <span className="truncate">
            {loading ? (
              <span className="flex items-center gap-1.5">
                <LoaderCircleIcon data-icon="inline-start" className="animate-spin" />
                加载项目...
              </span>
            ) : selected?.name ?? placeholder}
          </span>
        </ComboboxTrigger>
        <ComboboxContent>
          <ComboboxInput showTrigger={false} placeholder="搜索项目..." />
          <ComboboxEmpty>没有可用项目</ComboboxEmpty>
          <ComboboxList>
            {(group: ProjectContextGroup) => (
              <ComboboxGroup key={group.value} items={group.items}>
                <ComboboxLabel>{group.value}</ComboboxLabel>
                <ComboboxCollection>
                  {(item: ProjectContextItem) => (
                    <ComboboxItem key={item.id} value={item}>
                      {item.name}
                    </ComboboxItem>
                  )}
                </ComboboxCollection>
              </ComboboxGroup>
            )}
          </ComboboxList>
          {createProjectHref ? (
            <>
              <Separator />
              <Button
                variant="ghost"
                className="m-1 w-[calc(100%-0.5rem)] justify-start"
                render={<Link to={createProjectHref} />}
              >
                <PlusIcon data-icon="inline-start" />
                新建项目
              </Button>
            </>
          ) : null}
        </ComboboxContent>
      </Combobox>
    </div>
  )
}

export function ProjectContextControl(props: ProjectContextControlProps) {
  if (props.mode === 'hidden') return null
  if (props.mode === 'readonly') {
    return (
      <span
        data-slot="project-context-control"
        data-mode="readonly"
        className="inline-flex min-w-0 items-center truncate text-sm text-muted-foreground"
      >
        {props.project?.name ?? props.noProjectLabel ?? '未关联项目'}
      </span>
    )
  }
  return <SelectProjectContext {...props} />
}
