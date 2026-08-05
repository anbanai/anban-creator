import type { LucideIcon } from 'lucide-react'
import {
  Sparkles,
  Rss,
  CalendarRange,
  ListChecks,
  Settings,
  Coins,
  Terminal,
  LayoutGrid,
  Palette,
  Boxes,
} from 'lucide-react'

export interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  adminOnly?: boolean
}

export const mvpNavItems: NavItem[] = [
  { to: '/', label: 'AI助手', icon: Sparkles, end: true },
  { to: '/projects', label: '项目', icon: Rss },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/billing', label: '钱包', icon: Coins },
]

export const adminNavItems: NavItem[] = [
  { to: '/designer', label: '设计师', icon: Palette, adminOnly: true },
  { to: '/templates', label: '模板库', icon: LayoutGrid, adminOnly: true },
  { to: '/connect/claude-code', label: 'Claude Code', icon: Terminal, adminOnly: true },
  { to: '/connect/codex', label: 'Codex', icon: Boxes, adminOnly: true },
  { to: '/settings', label: '设置', icon: Settings, adminOnly: true },
]

export const allNavItems: NavItem[] = [
  ...mvpNavItems,
  ...adminNavItems,
]

export function visibleNavItems(items: NavItem[], isAdmin: boolean): NavItem[] {
  return items.filter((item) => !item.adminOnly || isAdmin)
}
