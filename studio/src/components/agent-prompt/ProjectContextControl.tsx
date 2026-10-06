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
import { cn } from '@/lib/utils'
import {
  ProjectIdentity,
  projectPlatformLabel,
  type ProjectIdentityProject,
} from './ProjectIdentity'

export interface ProjectContextProject extends ProjectIdentityProject {}

interface ProjectContextSelectProps {
  mode: 'select'
  projects: readonly ProjectContextProject[]
  value: string | null
  onValueChange: (id: string | null, project?: ProjectContextProject) => void
  allowNoProject?: boolean
  noProjectLabel?: string
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
  compact?: boolean
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
  name: string
}

type ProjectContextItem = ProjectItem | NoProjectItem

interface ProjectContextGroup {
  value: string
  items: ProjectContextItem[]
}

function isProjectContextItemEqual(item: ProjectContextItem, value: ProjectContextItem) {
  return item.kind === value.kind && item.id === value.id
}

function projectGroups(
  projects: readonly ProjectContextProject[],
  allowNoProject: boolean,
  noProjectItem: NoProjectItem,
): ProjectContextGroup[] {
  const result: ProjectContextGroup[] = []
  if (allowNoProject) result.push({ value: '上下文', items: [noProjectItem] })
  if (projects.length) result.push({ value: '项目', items: projects.map((project) => ({ ...project, kind: 'project' })) })
  return result
}

function SelectProjectContext({
  projects,
  value,
  onValueChange,
  allowNoProject = false,
  noProjectLabel = '不使用项目',
  createProjectHref,
  loading = false,
  disabled = false,
  placeholder = '选择项目...',
  compact = false,
  ariaLabel = '项目上下文',
}: ProjectContextSelectProps) {
  const noProjectItem = useMemo<NoProjectItem>(() => ({
    kind: 'none',
    id: '__no-project__',
    name: noProjectLabel,
  }), [noProjectLabel])
  const groups = useMemo(
    () => projectGroups(projects, allowNoProject, noProjectItem),
    [allowNoProject, noProjectItem, projects],
  )
  const projectItems = useMemo(
    () => groups.flatMap((group) => group.items),
    [groups],
  )
  const selected = useMemo(() => (
    value === null
      ? (allowNoProject ? noProjectItem : null)
      : projectItems.find((item) => item.kind === 'project' && item.id === value) ?? null
  ), [allowNoProject, noProjectItem, projectItems, value])
  const isDisabled = loading || disabled

  return (
    <div
      data-slot="project-context-control"
      data-mode="select"
      data-compact={compact}
      aria-busy={loading}
      className={cn(compact ? 'inline-flex min-w-0 max-w-full' : 'min-w-0')}
    >
      <Combobox
        items={groups}
        value={selected}
        disabled={isDisabled}
        itemToStringValue={(item: ProjectContextItem) => (
          item.kind === 'project'
            ? [item.name, item.description, item.positioning, item.instructions, projectPlatformLabel(item.platform), item.keywords].filter(Boolean).join(' ')
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
              variant={compact ? 'ghost' : 'outline'}
              aria-label={ariaLabel}
              disabled={isDisabled}
              className={cn(
                'min-w-0 justify-between font-normal',
                compact ? 'w-auto max-w-full' : 'w-full',
                compact ? 'h-8' : selected?.kind === 'project' ? 'h-auto min-h-14 py-2' : undefined,
                compact && 'px-1.5 shadow-none hover:bg-muted/70',
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
              <ProjectIdentity
                project={selected}
                compact={compact}
              />
            ) : selected?.name ?? placeholder}
          </span>
        </ComboboxTrigger>
        <ComboboxContent className="w-[min(36rem,calc(100vw-2rem))] min-w-0">
          <ComboboxInput showTrigger={false} placeholder="搜索项目..." />
          <ComboboxEmpty className="min-h-20 items-center">没有可用项目</ComboboxEmpty>
          <ComboboxList className="grid grid-cols-1 px-2 pb-2 pt-1 sm:grid-cols-2 sm:gap-x-2 sm:gap-y-1">
            {(group: ProjectContextGroup) => (
              <ComboboxGroup
                key={group.value}
                items={group.items}
                className="min-w-0"
              >
                <ComboboxLabel className="px-1.5 py-0.5">
                  <span className="truncate font-medium text-foreground/80">{group.value}</span>
                </ComboboxLabel>
                <ComboboxCollection>
                  {(item: ProjectContextItem) => (
                    <ComboboxItem
                      key={item.id}
                      value={item}
                      aria-label={item.kind === 'project'
                        ? [item.name, item.description, item.positioning, item.instructions, projectPlatformLabel(item.platform), item.keywords]
                          .filter(Boolean)
                          .join(' · ')
                        : item.name}
                      className="min-h-10 rounded-lg border border-transparent px-2 py-1 pr-9 aria-selected:border-border/70 aria-selected:bg-accent/70"
                    >
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
                  'mx-2 mb-2 mt-1 w-[calc(100%-1rem)] justify-center border border-dashed text-muted-foreground',
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
        data-compact={props.compact}
        className={cn(props.compact ? 'inline-flex min-w-0 max-w-full' : 'min-w-0')}
      >
        {props.project ? (
          <ProjectIdentity
            project={props.project}
            compact={props.compact}
          />
        ) : props.noProjectLabel ?? '未关联项目'}
      </div>
    )
  }
  return <SelectProjectContext {...props} />
}
