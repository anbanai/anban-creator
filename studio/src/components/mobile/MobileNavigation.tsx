import { GalleryHorizontalEnd, Sparkles, UserRound, CircleUserRound } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'

export function MobileNavigation() {
  const { pathname, state } = useLocation()
  const profileMode = pathname === '/' && (state as { modeId?: string } | null)?.modeId === 'profile'
  const items = [
    { to: '/mobile', label: '创作', icon: Sparkles, active: !profileMode && (pathname === '/' || pathname === '/mobile' || pathname === '/mobile/create' || pathname === '/tasks/new') },
    { to: '/mobile/personas', label: '人设', icon: UserRound, active: profileMode || pathname === '/mobile/personas' || pathname.startsWith('/portraits/') || pathname === '/projects' },
    { to: '/tasks', label: '作品', icon: GalleryHorizontalEnd, active: pathname.startsWith('/tasks') && pathname !== '/tasks/new' },
    { to: '/mobile/account', label: '我的', icon: CircleUserRound, active: ['/mobile/account', '/settings', '/billing', '/usage'].includes(pathname) },
  ]
  return <nav aria-label="手机主导航" className="mobile-bottom-nav">{items.map(item => <Link key={item.to} to={item.to} aria-current={item.active ? 'page' : undefined}><item.icon size={21} strokeWidth={item.active ? 2 : 1.6} /><span>{item.label}</span></Link>)}</nav>
}
