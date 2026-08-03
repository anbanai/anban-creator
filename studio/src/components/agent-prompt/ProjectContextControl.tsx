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
import { platformBgColor, platformBorderColor, renderPlatformIcon } from '@/lib/PlatformIcon'
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
  platform?: string
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
  const groups = new Map<string, ProjectItem[]>()
  for (const project of projects) {
    const platform = project.platform || 'project'
    const group = groups.get(platform) ?? []
    group.push({ ...project, kind: 'project' })
    groups.set(platform, group)
  }

  const result: ProjectContextGroup[] = []
  if (allowNoProject) result.push({ value: '上下文', items: [noProjectItem] })
  for (const [platform, items] of groups) {
    result.push({
      value: platformLabels[platform] ?? (platform === 'project' ? '项目' : platform),
      platform,
      items,
    })
  }
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
                selected?.kind === 'project' && selected.platform && [
                  'border-l-2',
                  platformBorderColor[selected.platform],
                ],
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
              <ProjectIdentity project={selected} compact={compact} showType={false} />
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
                  <span
                    data-platform={group.platform}
                    className="flex min-w-0 items-center gap-2"
                  >
                    {group.platform ? (
                      <span
                        className={cn(
                          'flex size-5 shrink-0 items-center justify-center rounded-md [&_svg]:size-3',
                          platformBgColor[group.platform],
                        )}
                      >
                        {renderPlatformIcon(group.platform)}
                      </span>
                    ) : null}
                    <span className="truncate font-medium text-foreground/80">{group.value}</span>
                  </span>
                </ComboboxLabel>
                <ComboboxCollection>
                  {(item: ProjectContextItem) => (
                    <ComboboxItem
                      key={item.id}
                      value={item}
                      className="min-h-10 rounded-lg border border-transparent px-2 py-1 pr-9 aria-selected:border-border/70 aria-selected:bg-accent/70"
                    >
                      {item.kind === 'project' ? (
                        <ProjectIdentity project={item} showType={false} />
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
        className="min-w-0"
      >
        {props.project ? (
          <ProjectIdentity project={props.project} compact={props.compact} showType={false} />
        ) : props.noProjectLabel ?? '未关联项目'}
      </div>
    )
  }
  return <SelectProjectContext {...props} />
}
