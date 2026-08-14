import type { LucideIcon } from 'lucide-react'
import {
  Sparkles,
  Rss,
  CalendarRange,
  ListChecks,
  Settings,
  Coins,
  LayoutGrid,
  PlugZap,
  KeyRound,
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
  { to: '/plugins', label: '插件', icon: PlugZap },
  { to: '/settings', label: '设置', icon: Settings },
]

export const adminNavItems: NavItem[] = [
  { to: '/templates', label: '模板库', icon: LayoutGrid, adminOnly: true },
  { to: '/admin/seednote', label: '小红书账号', icon: KeyRound, adminOnly: true },
]

export const allNavItems: NavItem[] = [
  ...mvpNavItems,
  ...adminNavItems,
]

export function visibleNavItems(items: NavItem[], isAdmin: boolean): NavItem[] {
  return items.filter((item) => !item.adminOnly || isAdmin)
}
