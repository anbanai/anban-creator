import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { LoaderCircleIcon, PlusIcon } from 'lucide-react'

import { Button, buttonVariants } from '@/components/ui/button'
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
import { cn } from '@/lib/utils'
import {
  ProjectIdentity,
  type ProjectIdentityProject,
} from './ProjectIdentity'

export interface ProjectContextProject extends ProjectIdentityProject {}

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
  compact?: boolean
  ariaLabel?: string
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

function isProjectContextItemEqual(item: ProjectContextItem, value: ProjectContextItem) {
  return item.kind === value.kind && item.id === value.id
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
  compact = false,
  ariaLabel = '项目上下文',
}: ProjectContextSelectProps) {
  const groups = useMemo(
    () => projectGroups(projects, allowNoProject),
    [allowNoProject, projects],
  )
  const projectItems = useMemo(
    () => groups.flatMap((group) => group.items),
    [groups],
  )
  const selected = useMemo(() => (
    value === null
      ? (allowNoProject ? NO_PROJECT_ITEM : null)
      : projectItems.find((item) => item.kind === 'project' && item.id === value) ?? null
  ), [allowNoProject, projectItems, value])
  const isDisabled = loading || disabled

  return (
    <div
      data-slot="project-context-control"
      data-mode="select"
      aria-busy={loading}
      className="min-w-0"
    >
      <Combobox
        items={groups}
        value={selected}
        disabled={isDisabled}
        itemToStringValue={(item: ProjectContextItem) => (
          item.kind === 'project'
            ? [item.name, item.description].filter(Boolean).join(' ')
            : item.name
        )}
        isItemEqualToValue={isProjectContextItemEqual}
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
              aria-label={ariaLabel}
              disabled={isDisabled}
              className={cn(
                'w-full min-w-0 justify-between font-normal',
                selected?.kind === 'project' && (compact ? 'h-8' : 'h-auto min-h-14 py-2'),
              )}
            />
          }
        >
          <span className="truncate">
            {loading ? (
              <span className="flex items-center gap-1.5">
                <LoaderCircleIcon data-icon="inline-start" className="animate-spin" />
                加载项目...
              </span>
            ) : selected?.kind === 'project' ? (
              <ProjectIdentity project={selected} compact={compact} />
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
                      {item.kind === 'project' ? (
                        <ProjectIdentity project={item} />
                      ) : item.name}
                    </ComboboxItem>
                  )}
                </ComboboxCollection>
              </ComboboxGroup>
            )}
          </ComboboxList>
          {createProjectHref ? (
            <>
              <Separator />
              <Link
                to={createProjectHref}
                className={cn(
                  buttonVariants({ variant: 'ghost' }),
                  'm-1 w-[calc(100%-0.5rem)] justify-start',
                )}
              >
                <PlusIcon data-icon="inline-start" />
                新建项目
              </Link>
            </>
          ) : null}
        </ComboboxContent>
      </Combobox>
      <span role="status" aria-live="polite" aria-atomic="true" className="sr-only">
        {loading ? '正在加载项目' : ''}
      </span>
    </div>
  )
}

export function ProjectContextControl(props: ProjectContextControlProps) {
  if (props.mode === 'hidden') return null
  if (props.mode === 'readonly') {
    return (
      <div
        data-slot="project-context-control"
        data-mode="readonly"
        className="min-w-0"
      >
        {props.project ? (
          <ProjectIdentity project={props.project} />
        ) : props.noProjectLabel ?? '未关联项目'}
      </div>
    )
  }
  return <SelectProjectContext {...props} />
}
