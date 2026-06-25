import type { LucideIcon } from 'lucide-react'
import {
  LayoutDashboard,
  Rss,
  CalendarRange,
  ListChecks,
  Clock,
  Settings,
  Coins,
  Activity,
  Terminal,
  Puzzle,
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

export const workflowItems: NavItem[] = [
  { to: '/', label: '仪表盘', icon: LayoutDashboard, end: true },
  { to: '/projects', label: '项目', icon: Rss },

  { to: '/designer', label: '设计师', icon: Palette },
  { to: '/templates', label: '模板库', icon: LayoutGrid },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/timeline', label: '时间轴', icon: Clock },
]

export const analyticsItems: NavItem[] = [
  { to: '/credits', label: '积分', icon: Coins },
  { to: '/usage', label: '用量', icon: Activity },
]

export const platformItems: NavItem[] = [
  { to: '/connect/claude-code', label: 'Claude Code', icon: Terminal },
  { to: '/connect/openclaw', label: 'OpenClaw', icon: Puzzle },
  { to: '/connect/codex', label: 'Codex', icon: Boxes },
]

export const allNavItems: NavItem[] = [
  ...workflowItems,
  ...analyticsItems,
  ...platformItems,
  { to: '/settings', label: '设置', icon: Settings },
]
