import { NavLink } from 'react-router-dom'
import { useTheme } from 'next-themes'
import { useEffect, useState } from 'react'
import {
  LayoutDashboard,
  Rss,
  CalendarRange,
  ListChecks,
  Clock,
  Settings,
  Sun,
  Moon,
  Coins,
  Menu,
  X,
  Activity,
} from 'lucide-react'
import UserAccountPopover from '@/components/auth/UserAccountPopover'

const navItems = [
  { to: '/', label: '仪表盘', icon: LayoutDashboard, end: true },
  { to: '/channels', label: '渠道', icon: Rss },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/timeline', label: '时间轴', icon: Clock },
  { to: '/credits', label: '积分', icon: Coins },
  { to: '/usage', label: '用量', icon: Activity },
]

const platformItems = [
  {
    to: '/connect/claude-code',
    label: 'Claude Code',
    icon: () => (
      <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="currentColor">
        <path d="M17.303 2.244l-3.534 9.858-4.016-4.016 2.79-6.996A8.934 8.934 0 0121.617 0a8.934 8.934 0 018.59 1.09l2.79 6.996-4.016 4.016-3.534-9.858c-.17-.47-.57-.47-.57 0 0-6.765 1.75-10.09 5.36a14.94 14.94 0 00-1.34 11.24c0 5.73 4.18 10.49 9.66 11.39a14.17 14.17 0 007.05-2.11c2.79-2.06 4.58-5.16 4.58-8.82v-1.5l-5.37 2.27 2.62-6.28z" />
      </svg>
    ),
  },
  {
    to: '/connect/openclaw',
    label: 'OpenClaw',
    icon: () => (
      <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1.41 14.59l-2.12-2.12 2.83-2.83-2.83-2.83 2.12-2.12L12 8.41l1.41-1.41-2.83-2.83 2.83-2.83-1.41-1.41L12 4.17l-1.41 1.41-2.83 2.83 2.83 2.83-2.12 2.12L12 15.59l1.41 1.41z" />
      </svg>
    ),
  },
]

function ThemeToggle() {
  const { setTheme, resolvedTheme } = useTheme()
  const [mounted, setMounted] = useState(false)

  useEffect(() => setMounted(true), [])

  if (!mounted) {
    return <div className="h-8 w-8" />
  }

  const isDark = resolvedTheme === 'dark'

  return (
    <button
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors duration-150 hover:bg-sidebar-accent hover:text-sidebar-foreground"
      title={isDark ? '切换到亮色模式' : '切换到暗色模式'}
    >
      {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  )
}

export default function Sidebar() {
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <>
      {/* Mobile hamburger */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed left-4 top-4 z-50 flex h-10 w-10 items-center justify-center rounded-lg bg-sidebar text-sidebar-foreground shadow-lg md:hidden"
        aria-label="打开菜单"
      >
        <Menu className="h-5 w-5" />
      </button>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm md:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Mobile close button */}
      {mobileOpen && (
        <button
          onClick={() => setMobileOpen(false)}
          className="fixed left-[196px] top-4 z-50 flex h-8 w-8 items-center justify-center rounded-md bg-sidebar text-muted-foreground transition-colors hover:text-sidebar-foreground md:hidden"
          aria-label="关闭菜单"
        >
          <X className="h-4 w-4" />
        </button>
      )}

      {/* Sidebar */}
      <aside
        className={`fixed left-0 top-0 z-40 flex h-screen w-[220px] flex-col border-r border-sidebar-border bg-sidebar transition-transform duration-200 md:static md:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        {/* Logo */}
        <div className="flex h-14 items-center px-5">
          <span className="text-base font-bold tracking-tight text-sidebar-foreground">
            案板创作助手
          </span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 space-y-0.5 px-3 pt-2">
          {platformItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                  isActive
                    ? 'border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]'
                    : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground'
                }`
              }
            >
              <item.icon />
              {item.label}
            </NavLink>
          ))}
          <div className="my-2 border-t border-sidebar-border" />
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                  isActive
                    ? 'border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]'
                    : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground'
                }`
              }
            >
              <item.icon className="h-4 w-4 shrink-0" />
              {item.label}
            </NavLink>
          ))}
        </nav>

        {/* Divider */}
        <div className="mx-3 border-t border-sidebar-border" />

        {/* Bottom: Settings */}
        <div className="px-3 py-2 space-y-0.5">
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                isActive
                  ? 'border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]'
                  : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground'
              }`
            }
          >
            <Settings className="h-4 w-4 shrink-0" />
            设置
          </NavLink>
        </div>

        {/* Bottom: Notifications + User + Theme */}
        <div className="flex items-center gap-1 border-t border-sidebar-border px-3 py-3">
          <UserAccountPopover />
          <ThemeToggle />
        </div>
      </aside>
    </>
  )
}
