import { BuildVersion } from '@/components/BuildVersion'
import { Link } from 'react-router-dom'
import { ArrowUpRight } from 'lucide-react'
import './AuthLayout.css'

export default function AuthLayout({
  children,
  title,
  subtitle,
  footerText,
  footerLinkText,
  footerLinkTo,
}: {
  children: React.ReactNode
  title: string
  subtitle: string
  footerText: string
  footerLinkText: string
  footerLinkTo: string
}) {
  return (
    <div className="auth-page dark relative flex min-h-dvh items-center overflow-hidden">
      <div className="auth-brand-panel">
        <Link to="/" className="auth-brand-panel__logo" aria-label="返回 Anban AI 官网">
          <img src="/brand/anban-horizontal.png" alt="Anban AI" />
        </Link>
        <p className="auth-brand-panel__eyebrow">CONTENT OPERATING SYSTEM</p>
        <h1>内容生产，<br /><span>持续进化。</span></h1>
        <p className="auth-brand-panel__description">从选题判断、内容与素材生成，到发布记录和效果复盘，让每一轮创作都成为下一轮的依据。</p>
        <Link to="/" className="auth-brand-panel__back">探索 Anban AI <ArrowUpRight className="size-4" /></Link>
        <div className="auth-brand-panel__signature"><span>ANBAN</span><span>01 — 04</span></div>
      </div>

      <div className="auth-form-zone">
        <div className="auth-mobile-brand">
          <Link to="/" aria-label="返回 Anban AI 官网"><img src="/brand/anban-horizontal.png" alt="Anban AI" /></Link>
        </div>
        <div className="auth-form-panel">
          <div className="auth-form-panel__index">STUDIO / ACCOUNT</div>
          <div className="mb-6 text-left">
            <h2 className="text-xl font-semibold tracking-tight text-foreground">{title}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
          </div>
          {children}
          <p className="mt-5 text-center text-sm text-muted-foreground">
            {footerText}{' '}
            <Link to={footerLinkTo} className="font-medium text-primary hover:text-primary/80">
              {footerLinkText}
            </Link>
          </p>
        </div>
      </div>

      <div className="auth-footer">
        <span><BuildVersion /></span>
        <span>© {new Date().getFullYear()} 成都北冕星辰科技有限公司</span>
        <a href="https://beian.miit.gov.cn/" target="_blank" rel="noreferrer">蜀ICP备2024071665号-3</a>
      </div>
    </div>
  )
}
