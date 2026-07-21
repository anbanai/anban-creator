import type { LucideIcon } from 'lucide-react'
import {
  Sparkles,
  Rss,
  CalendarRange,
  ListChecks,
  Clock,
  Settings,
  Coins,
  Activity,
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
}

export const todayItems: NavItem[] = [
  { to: '/', label: 'AI助手', icon: Sparkles, end: true },
]

export const creationItems: NavItem[] = [
  { to: '/projects', label: '项目', icon: Rss },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/designer', label: '设计师', icon: Palette },
]

export const automationItems: NavItem[] = [
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/timeline', label: '时间轴', icon: Clock },
]

export const assetItems: NavItem[] = [
  { to: '/templates', label: '模板库', icon: LayoutGrid },
]

export const businessItems: NavItem[] = [
  { to: '/billing', label: '钱包', icon: Coins },
  { to: '/usage', label: '用量', icon: Activity },
]

export const platformItems: NavItem[] = [
  { to: '/connect/claude-code', label: 'Claude Code', icon: Terminal },
  { to: '/connect/codex', label: 'Codex', icon: Boxes },
]

export const connectSettingItems: NavItem[] = [
  ...platformItems,
  { to: '/settings', label: '设置', icon: Settings },
]

export const workflowItems: NavItem[] = [
  ...todayItems,
  ...creationItems,
  ...automationItems.filter((item) => item.to !== '/plans'),
  ...assetItems,
]

export const analyticsItems: NavItem[] = businessItems

export const allNavItems: NavItem[] = [
  ...todayItems,
  ...creationItems,
  ...automationItems,
  ...assetItems,
  ...businessItems,
  ...connectSettingItems,
]
