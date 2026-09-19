import { Fragment, useEffect, useState, useSyncExternalStore } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import type { LucideIcon } from 'lucide-react'
import { Menu, X, PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react'
import UserAccountPopover from '@/components/auth/UserAccountPopover'
import { Button } from '@/components/ui/button'
import { Sheet, SheetTrigger, SheetContent, SheetTitle, SheetClose } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useAuth } from '@/contexts/AuthContext'
import { adminNavItems, mvpNavItems, allNavItems } from '@/lib/navigation'
import { commandPaletteStore, commandPaletteAccelerator } from '@/lib/command-palette'

function useCollapsedState() {
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem('sidebar-collapsed') === 'true' } catch { return false }
  })
  useEffect(() => {
    try { localStorage.setItem('sidebar-collapsed', String(collapsed)) } catch { /* Storage may be unavailable. */ }
  }, [collapsed])
  return [collapsed, setCollapsed] as const
}

export default function Sidebar() {
  const { user } = useAuth()
  const location = useLocation()
  const { pathname } = location
  const [mobileOpen, setMobileOpen] = useState(false)
  const paletteOpen = useSyncExternalStore(commandPaletteStore.subscribe, commandPaletteStore.getSnapshot)
  const [collapsed, setCollapsed] = useCollapsedState()
  const currentPage = allNavItems.find(item => item.to === pathname)?.label
    ?? (pathname.startsWith('/tasks/') ? '任务详情' : '工作空间')

  // Account links and keyboard shortcuts can navigate outside the drawer's own links.
  useEffect(() => { setMobileOpen(false) }, [location])
  useEffect(() => { if (paletteOpen) setMobileOpen(false) }, [paletteOpen])

  // A drawer is temporary navigation; switching to desktop must release its focus trap.
  useEffect(() => {
    const media = window.matchMedia('(min-width: 768px)')
    const closeOnDesktop = () => { if (media.matches) setMobileOpen(false) }
    media.addEventListener('change', closeOnDesktop)
    return () => media.removeEventListener('change', closeOnDesktop)
  }, [])

  const contents = (compact: boolean, mobile = false) => (
    <>
      <div className={`flex h-20 shrink-0 items-center gap-2 ${compact ? 'justify-center px-2' : 'px-5'}`}>
        {compact ? (
          <Button variant="ghost" size="icon" onClick={() => setCollapsed(false)} aria-label="展开侧边栏" aria-expanded={false}>
            <PanelLeftOpen />
          </Button>
        ) : (
          <>
            <div className="min-w-0 flex-1">
              <p className="text-xl font-semibold tracking-tight">Anban<span className="ml-1.5 text-primary">安般</span></p>
              <p className="mt-0.5 text-xs text-muted-foreground">智能创作助手</p>
            </div>
            {mobile ? (
              <SheetClose render={<Button variant="ghost" size="icon" aria-label="关闭菜单" />}><X /></SheetClose>
            ) : (
              <Button variant="ghost" size="icon" onClick={() => setCollapsed(true)} aria-label="收起侧边栏" aria-expanded={true}><PanelLeftClose /></Button>
            )}
          </>
        )}
      </div>
      <div className="px-3 pb-3">
        <button
          type="button"
          aria-label={`搜索 (${commandPaletteAccelerator})`}
          onClick={() => { setMobileOpen(false); commandPaletteStore.open() }}
          className={`flex min-h-10 w-full items-center gap-2 rounded-lg border border-sidebar-border bg-card px-3 text-sm text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground ${compact ? 'justify-center px-0' : ''}`}
        >
          <Search className="size-4 shrink-0" />
          {!compact && <><span className="flex-1 text-left">搜索与快捷操作</span><kbd className="rounded border border-border px-1 text-[10px]">{commandPaletteAccelerator}</kbd></>}
        </button>
      </div>
      <nav className="min-h-0 flex-1 overflow-y-auto px-3 pb-4" aria-label="主导航">
        <div className="flex flex-col gap-1">
          {mvpNavItems.map((item, index) => (
            <Fragment key={item.to}>
              {[0, 4, 7].includes(index) && (compact
                ? index > 0 && <div className="mx-2 my-2 border-t border-sidebar-border" />
                : <p className={`px-3 pb-1.5 text-[11px] font-medium text-muted-foreground ${index > 0 ? 'pt-5' : 'pt-2'}`}>{index === 0 ? '工作空间' : index === 4 ? '管理与接入' : '内容数据'}</p>
              )}
              <SidebarNavLink item={item} collapsed={compact} onClick={() => setMobileOpen(false)} />
            </Fragment>
          ))}
        </div>
        {user?.is_admin && (
          <div className="mt-4 flex flex-col gap-1 border-t border-sidebar-border pt-3">
            {!compact && <p className="px-3 pb-1 text-[11px] font-medium text-muted-foreground">管理工具</p>}
            {adminNavItems.map(item => <SidebarNavLink key={item.to} item={item} collapsed={compact} onClick={() => setMobileOpen(false)} />)}
          </div>
        )}
      </nav>
      <div className={`shrink-0 border-t border-sidebar-border py-3 ${compact ? 'flex justify-center' : 'px-3'}`}>
        <UserAccountPopover collapsed={compact} />
      </div>
    </>
  )

  return (
    <>
      <header className="fixed inset-x-0 top-0 z-30 flex h-14 items-center gap-3 border-b border-border bg-background/95 px-3 backdrop-blur md:hidden">
        <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
          <SheetTrigger render={<Button variant="ghost" size="icon" aria-label="打开菜单" />}><Menu /></SheetTrigger>
          <SheetContent side="left" showCloseButton={false} className="w-[280px] max-w-[calc(100vw-3rem)] gap-0 bg-sidebar" aria-describedby={undefined} finalFocus={() => !commandPaletteStore.getSnapshot()}>
            <SheetTitle className="sr-only">导航菜单</SheetTitle>
            {contents(false, true)}
          </SheetContent>
        </Sheet>
        <span className="min-w-0 flex-1 truncate text-sm font-semibold">{currentPage}</span>
      </header>
      <aside aria-label="工作空间导航" className={`hidden h-dvh shrink-0 flex-col border-r border-sidebar-border bg-sidebar transition-[width] duration-200 md:flex ${collapsed ? 'w-[72px]' : 'w-[240px]'}`}>
        {contents(collapsed)}
      </aside>
    </>
  )
}

function SidebarNavLink({ item, collapsed, onClick }: {
  item: { to: string; label: string; icon: LucideIcon; end?: boolean }
  collapsed: boolean
  onClick: () => void
}) {
  const link = (
    <NavLink to={item.to} end={item.end} onClick={onClick} aria-label={collapsed ? item.label : undefined}
      className={({ isActive }) => `flex min-h-10 items-center gap-3 rounded-lg px-3 text-sm transition-colors ${isActive ? 'bg-sidebar-accent font-semibold text-sidebar-primary' : 'font-medium text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground'} ${collapsed ? 'justify-center px-0' : ''}`}
    >
      <item.icon className="size-[18px] shrink-0" />
      {!collapsed && item.label}
    </NavLink>
  )
  return collapsed ? <Tooltip><TooltipTrigger render={link} /><TooltipContent side="right">{item.label}</TooltipContent></Tooltip> : link
}
