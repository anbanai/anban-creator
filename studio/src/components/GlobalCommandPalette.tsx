import { useEffect, useCallback, useSyncExternalStore } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTheme } from 'next-themes'
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

const shortcutMap: Record<string, string> = {
  '仪表盘': 'g d',
  '项目': 'g c',
  '计划': 'g p',
  '任务': 'g t',
  '时间轴': 'g l',
  '积分': 'g $',
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
    <CommandDialog open={open} onOpenChange={setOpen} title="命令面板" description="搜索命令或导航">
      <CommandInput placeholder="输入命令或搜索..." />
      <CommandList>
        <CommandEmpty>没有找到匹配项</CommandEmpty>

        <CommandGroup heading="导航">
          {allNavItems.map((item) => (
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

        <CommandGroup heading="快捷操作">
          <CommandItem onSelect={() => handleSelect(() => navigate('/tasks?create=true'))}>
            <span className="mr-2 text-primary">+</span>
            新建任务
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => navigate('/projects'))}>
            <span className="mr-2 text-primary">+</span>
            新建项目
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('light'))}>
            亮色模式
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('dark'))}>
            暗色模式
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme('system'))}>
            跟随系统
            <CommandShortcut>Theme</CommandShortcut>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
