import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowUpRight, CircleHelp, LogOut, Settings, Wallet, UserRound } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { BuildVersion } from '@/components/BuildVersion'
import { Button } from '@/components/ui/button'

export default function MobileAccountPage() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [leaving, setLeaving] = useState(false)
  const [error, setError] = useState('')
  return <div className="mobile-studio-page"><div className="mobile-page-eyebrow">YOUR SPACE</div><header className="mobile-account-hero"><span className="mobile-persona-avatar"><UserRound size={29} /></span><h1>{user?.nickname || '我的创作空间'}</h1><p>把每一次灵感，变成自己的作品。</p></header>
    <div className="mobile-account-links">{[{ to: '/billing', title: '钱包与账单', detail: '查看余额和创作消耗', icon: Wallet }, { to: '/settings', title: '账号设置', detail: '管理账号与个人偏好', icon: Settings }, { to: '/projects', title: '更多工作空间功能', detail: '归档、排期、素材与内容分析', icon: CircleHelp }].map(item => <Link key={item.to} to={item.to}><item.icon size={20} strokeWidth={1.6} /><span><strong>{item.title}</strong><small>{item.detail}</small></span><ArrowUpRight size={17} /></Link>)}</div>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    <Button variant="ghost" className="mobile-logout" disabled={leaving} onClick={async () => { if (leaving) return; setLeaving(true); try { await logout(); navigate('/login', { replace: true }) } catch { setError('退出失败，请重试。'); setLeaving(false) } }}><LogOut size={16} />{leaving ? '正在退出…' : '退出登录'}</Button>
    <footer className="mobile-account-version"><span>ANBAN AI · 内容智能创作</span><BuildVersion /></footer>
  </div>
}
