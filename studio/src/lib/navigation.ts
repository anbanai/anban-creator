import type { LucideIcon } from 'lucide-react'
import {
  Sparkles,
  Rss,
  CalendarRange,
  CalendarDays,
  ListChecks,
  Settings,
  Coins,
  LayoutGrid,
  PlugZap,
  KeyRound,
  BarChart3,
  Flame,
  MessageCircle,
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
  { to: '/projects/new/interview', label: '新手引导', icon: MessageCircle, end: true },
  { to: '/projects', label: '项目', icon: Rss },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/billing', label: '钱包', icon: Coins },
  { to: '/plugins', label: '插件', icon: PlugZap },
  { to: '/settings', label: '设置', icon: Settings },
  { to: '/content-analytics', label: '内容分析', icon: BarChart3 },
  { to: '/trends', label: '热点雷达', icon: Flame },
]

export const workspaceNavItems: NavItem[] = [
  ...mvpNavItems.slice(0, 5),
  { to: '/timeline', label: '时间线', icon: CalendarDays, adminOnly: true },
  ...mvpNavItems.slice(5),
]

export const adminNavItems: NavItem[] = [
  { to: '/templates', label: '模板库', icon: LayoutGrid, adminOnly: true },
  { to: '/admin/seednote', label: '种草笔记账号', icon: KeyRound, adminOnly: true },
]

export const allNavItems: NavItem[] = [
  ...workspaceNavItems,
  ...adminNavItems,
]

export function visibleNavItems(items: NavItem[], isAdmin: boolean): NavItem[] {
  return items.filter((item) => !item.adminOnly || isAdmin)
}
