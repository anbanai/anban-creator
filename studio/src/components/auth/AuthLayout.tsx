import { Link } from 'react-router-dom'

const featurePills = [
  { label: 'AI 智能写作', icon: '✍️' },
  { label: '一键生图', icon: '🎨' },
  { label: '直接发布', icon: '📱' },
  { label: 'SEO 优化', icon: '🔍' },
]

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
    <div className="relative flex min-h-screen items-center justify-center xl:justify-end overflow-hidden bg-gradient-to-br from-amber-50 via-amber-100 to-amber-300 dark:from-amber-950 dark:via-amber-900 dark:to-amber-800">
      {/* Decorative circles */}
      <div className="pointer-events-none absolute -right-16 -top-16 h-52 w-52 rounded-full bg-amber-400/30 dark:bg-amber-600/20" />
      <div className="pointer-events-none absolute -bottom-10 right-80 h-36 w-36 rounded-full bg-amber-500/20 dark:bg-amber-700/20" />
      <div className="pointer-events-none absolute left-[60%] top-20 h-20 w-20 rounded-full bg-amber-600/15 dark:bg-amber-800/15" />

      {/* Hero section — desktop only */}
      <div className="absolute inset-y-0 left-0 hidden w-[55%] flex-col justify-center px-16 xl:flex">
        <div className="mb-4 text-sm font-semibold tracking-widest text-amber-800 dark:text-amber-300">
          ✦ 案板创作助手
        </div>
        <h1 className="text-5xl font-black leading-tight text-amber-900 dark:text-amber-100">
          让创作
          <br />
          更简单
        </h1>
        <p className="mt-4 max-w-sm text-base leading-relaxed text-amber-700 dark:text-amber-300">
          从灵感到发布，AI 全程陪伴你的内容创作之旅。微信公众号、小红书，一站搞定。
        </p>
        <div className="mt-6 flex flex-wrap gap-2">
          {featurePills.map((pill) => (
            <span
              key={pill.label}
              className="rounded-full border border-amber-300/60 bg-white/70 px-3.5 py-1.5 text-xs font-medium text-amber-800 backdrop-blur-sm dark:border-amber-600/40 dark:bg-amber-800/40 dark:text-amber-200"
            >
              {pill.icon} {pill.label}
            </span>
          ))}
        </div>
      </div>

      {/* Form card */}
      <div className="relative z-10 mx-4 w-full max-w-[380px] rounded-2xl bg-white p-8 shadow-2xl shadow-amber-900/10 dark:bg-amber-950 dark:shadow-black/30 sm:mx-0 xl:mr-24">
        {/* Mobile brand header */}
        <div className="mb-6 text-center xl:hidden">
          <div className="text-xs font-semibold tracking-widest text-amber-700 dark:text-amber-400">
            ✦ 案板创作助手
          </div>
          <div className="mt-2 text-2xl font-black text-amber-900 dark:text-amber-100">让创作更简单</div>
        </div>

        {/* Form header */}
        <div className="mb-6 text-center">
          <h2 className="text-lg font-bold text-foreground">{title}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
        </div>

        {/* Form content slot */}
        {children}

        {/* Footer link */}
        <p className="mt-5 text-center text-sm text-muted-foreground">
          {footerText}{' '}
          <Link to={footerLinkTo} className="font-medium text-primary hover:text-primary/80">
            {footerLinkText}
          </Link>
        </p>
      </div>
    </div>
  )
}
