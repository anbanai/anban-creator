import { useEffect, useCallback, useMemo, useSyncExternalStore } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTheme } from 'next-themes'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, FileText, FolderPlus, ListChecks, Moon, Plus, Search, Settings, Sun, WandSparkles } from 'lucide-react'
import {
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandSeparator,
  CommandShortcut,
} from '@/components/ui/command'
import { allNavItems, type NavItem } from '@/lib/navigation'
import { commandPaletteStore } from '@/lib/command-palette'
import { api } from '@/lib/api'
import { buildCommandCenterSignals, buildNextBestActions, createTaskHref, projectsReturnHref } from '@/lib/command-center'
import { queryKeys } from '@/lib/query-keys'
import { useAuth } from '@/contexts/AuthContext'

const shortcutMap: Record<string, string> = {
  '首页': 'g d',
  '项目': 'g c',
  '计划': 'g p',
  '任务': 'g t',
  '时间轴': 'g l',
  '钱包': 'g $',
  '设置': 'g s',
}

function findShortcut(label: string): string | undefined {
  return shortcutMap[label]
}

function ShortcutLabel({ item }: { item: NavItem }) {
  const shortcut = findShortcut(item.label)
  if (!shortcut) return null
  return <CommandShortcut>{shortcut}</CommandShortcut>
}

export default function GlobalCommandPalette() {
  const open = useSyncExternalStore(commandPaletteStore.subscribe, commandPaletteStore.getSnapshot)
  const navigate = useNavigate()
  const { setTheme } = useTheme()
  const { user } = useAuth()

  const { data: tasksData } = useQuery({
    queryKey: ['command-palette', 'tasks'],
    queryFn: () => api.tasks.list({ limit: 20 }),
    enabled: open,
  })
  const { data: projects = [] } = useQuery({
    queryKey: ['command-palette', 'projects'],
    queryFn: () => api.projects.list({ status: 'active' }),
    enabled: open,
    staleTime: 60_000,
  })
  const { data: plansData } = useQuery({
    queryKey: ['command-palette', 'plans'],
    queryFn: () => api.plans.list({ limit: 20 }),
    enabled: open,
  })
  const { data: billingWallet } = useQuery({
    queryKey: ['command-palette', 'billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
    enabled: open,
  })
  const { data: apiKeys = [] } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
    enabled: open,
  })
  const tasks = tasksData?.items ?? []
  const plans = plansData?.items ?? []
  const signals = useMemo(() => buildCommandCenterSignals({
    tasks,
    plans,
    projects,
    billingWallet,
    apiKeysReady: apiKeys.length > 0,
    localExecutorReady: true,
  }), [tasks, plans, projects, billingWallet, apiKeys.length])
  const nextActions = useMemo(() => buildNextBestActions(signals), [signals])
  const failedTasks = signals.failedTasks.slice(0, 5)
  const defaultProject = signals.projects[0]
  const navigationItems = useMemo(
    () => user?.is_admin ? allNavItems : allNavItems.filter((item) => item.to !== '/templates'),
    [user?.is_admin],
  )

  const setOpen = useCallback((v: boolean) => {
    if (v) commandPaletteStore.open()
    else commandPaletteStore.close()
  }, [])

  const handleSelect = useCallback((action: () => void) => {
    commandPaletteStore.close()
    action()
  }, [])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        const tag = (e.target as HTMLElement).tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return
        e.preventDefault()
        commandPaletteStore.toggle()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  return (
    <CommandDialog open={open} onOpenChange={setOpen} title="行动面板" description="搜索命令、任务或下一步动作">
      <CommandInput placeholder="搜索动作、任务、页面..." />
      <CommandList>
        <CommandEmpty>没有找到匹配项</CommandEmpty>

        <CommandGroup heading="继续工作">
          {nextActions.slice(0, 4).map((action) => (
            <CommandItem
              key={action.id}
              onSelect={() => handleSelect(() => navigate(action.href))}
            >
              <WandSparkles className="mr-2 h-4 w-4" />
              {action.label}
            </CommandItem>
          ))}
          {nextActions.length === 0 && (
            <CommandItem onSelect={() => handleSelect(() => navigate(createTaskHref({ type: defaultProject?.platform, projectId: defaultProject?.id, intent: 'new' })))}>
              <Plus className="mr-2 h-4 w-4" />
              新建创作任务
            </CommandItem>
          )}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="创建">
          <CommandItem onSelect={() => handleSelect(() => navigate(createTaskHref({ type: 'article', projectId: defaultProject?.platform === 'article' ? defaultProject.id : undefined, intent: 'new' })))}>
            <FileText className="mr-2 h-4 w-4" />
            新建公众号文章
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => navigate(createTaskHref({ type: 'seednote', projectId: defaultProject?.platform === 'seednote' ? defaultProject.id : undefined, intent: 'new' })))}>
            <Plus className="mr-2 h-4 w-4" />
            新建种草笔记
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => navigate('/plans?create=true&type=article&intent=schedule'))}>
            <ListChecks className="mr-2 h-4 w-4" />
            创建自动计划
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => navigate(projectsReturnHref({ type: 'seednote', intent: 'new' })))}>
            <FolderPlus className="mr-2 h-4 w-4" />
            新建项目
          </CommandItem>
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="恢复">
          {failedTasks.map((task) => (
            <CommandItem key={task.id} onSelect={() => handleSelect(() => navigate(`/tasks/${task.id}`))}>
              <AlertTriangle className="mr-2 h-4 w-4" />
              {task.title || task.prompt || '失败任务'}
            </CommandItem>
          ))}
          <CommandItem onSelect={() => handleSelect(() => navigate('/tasks?status=failed'))}>
            <AlertTriangle className="mr-2 h-4 w-4" />
            打开失败队列
          </CommandItem>
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="跳转">
          {navigationItems.map((item) => (
            <CommandItem
              key={item.to}
              onSelect={() => handleSelect(() => navigate(item.to))}
            >
              <item.icon className="mr-2 h-4 w-4" />
              {item.label}
              <ShortcutLabel item={item} />
            </CommandItem>
          ))}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="设置">
          <CommandItem onSelect={() => handleSelect(() => navigate('/settings'))}>
            <Settings className="mr-2 h-4 w-4" />
            打开接入就绪中心
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('light'))}>
            <Sun className="mr-2 h-4 w-4" />
            亮色模式
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('dark'))}>
            <Moon className="mr-2 h-4 w-4" />
            暗色模式
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('system'))}>
            <Search className="mr-2 h-4 w-4" />
            跟随系统
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
