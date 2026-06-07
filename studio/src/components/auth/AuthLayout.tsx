import { useRef } from 'react'
import { Link } from 'react-router-dom'
import gsap from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

const featurePills = [
  { label: 'AI 智能写作', icon: '✍️' },
  { label: '一键生图', icon: '🎨' },
  { label: '直接发布', icon: '📱' },
  { label: 'SEO 优化', icon: '🔍' },
]

const TITLE_LINE_1 = '让创作'
const TITLE_LINE_2 = '更简单'

function TitleChars({ text }: { text: string }) {
  return (
    <>
      {text.split('').map((char, i) => (
        <span key={i} className="inline-block" style={{ willChange: 'transform, opacity' }}>
          {char}
        </span>
      ))}
    </>
  )
}

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
  const containerRef = useRef<HTMLDivElement>(null)

  useGSAP(() => {
    const tl = gsap.timeline({
      defaults: { ease: 'power3.out' },
    })

    // Circles fade in
    tl.from('[data-animate="circle"]', {
      scale: 0,
      opacity: 0,
      duration: 0.8,
      stagger: 0.15,
      ease: 'back.out(1.7)',
    })

    // Brand tag
    tl.from('[data-animate="brand"]', {
      x: -30,
      opacity: 0,
      duration: 0.6,
    }, '-=0.4')

    // Title line 1 characters
    tl.from('[data-animate="title-line-1"] span', {
      y: 50,
      opacity: 0,
      rotationX: -40,
      duration: 0.6,
      stagger: 0.06,
      ease: 'back.out(1.7)',
    }, '-=0.3')

    // Title line 2 characters
    tl.from('[data-animate="title-line-2"] span', {
      y: 50,
      opacity: 0,
      rotationX: -40,
      duration: 0.6,
      stagger: 0.06,
      ease: 'back.out(1.7)',
    }, '-=0.3')

    // Description
    tl.from('[data-animate="desc"]', {
      y: 15,
      opacity: 0,
      duration: 0.5,
    }, '-=0.3')

    // Feature pills stagger
    tl.from('[data-animate="pill"]', {
      y: 20,
      opacity: 0,
      scale: 0.9,
      duration: 0.5,
      stagger: 0.08,
      ease: 'back.out(1.4)',
    }, '-=0.2')

    // Form card
    tl.from('[data-animate="form-card"]', {
      x: 60,
      opacity: 0,
      duration: 0.7,
      ease: 'power3.out',
    }, '-=0.5')

    // Mobile brand (only visible on mobile, so this is fine)
    tl.from('[data-animate="mobile-brand"]', {
      y: -15,
      opacity: 0,
      duration: 0.4,
    }, '-=0.5')

    // Footer
    tl.from('[data-animate="footer"]', {
      opacity: 0,
      duration: 0.6,
    }, '-=0.3')

    // Continuous floating for circles (separate from timeline)
    const circles = containerRef.current?.querySelectorAll('[data-animate="circle"]')
    circles?.forEach((circle, i) => {
      gsap.to(circle, {
        y: `+=${12 + i * 5}`,
        x: `+=${(i % 2 === 0 ? 1 : -1) * (5 + i * 3)}`,
        duration: 4 + i * 1.5,
        ease: 'sine.inOut',
        repeat: -1,
        yoyo: true,
        delay: 1 + i * 0.3,
      })
    })

    // Subtle pulse on title characters after entrance
    gsap.to('[data-animate="title-line-1"] span, [data-animate="title-line-2"] span', {
      textShadow: '0 0 20px rgba(217, 119, 6, 0.3)',
      duration: 2,
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
      delay: 2,
      stagger: { each: 0.1, from: 'random' },
    })
  }, { scope: containerRef })

  return (
    <div
      ref={containerRef}
      className="relative flex min-h-screen items-center justify-center overflow-hidden bg-gradient-to-br from-amber-50 via-amber-100 to-amber-300 dark:from-amber-950 dark:via-amber-900 dark:to-amber-800 xl:justify-end"
    >
      {/* Decorative circles */}
      <div
        data-animate="circle"
        className="pointer-events-none absolute -right-16 -top-16 h-52 w-52 rounded-full bg-amber-400/30 dark:bg-amber-600/20"
      />
      <div
        data-animate="circle"
        className="pointer-events-none absolute -bottom-10 right-80 h-36 w-36 rounded-full bg-amber-500/20 dark:bg-amber-700/20"
      />
      <div
        data-animate="circle"
        className="pointer-events-none absolute left-[60%] top-20 h-20 w-20 rounded-full bg-amber-600/15 dark:bg-amber-800/15"
      />

      {/* Hero section — desktop only */}
      <div className="absolute inset-y-0 left-0 hidden w-[55%] flex-col justify-center px-16 xl:flex">
        <div
          data-animate="brand"
          className="mb-4 text-sm font-semibold tracking-widest text-amber-800 dark:text-amber-300"
          style={{ willChange: 'transform, opacity' }}
        >
          ✦ Anban 智能创作助手
        </div>
        <h1 className="text-5xl font-black leading-tight text-amber-900 dark:text-amber-100" style={{ perspective: '600px' }}>
          <span data-animate="title-line-1" className="inline-block">
            <TitleChars text={TITLE_LINE_1} />
          </span>
          <br />
          <span data-animate="title-line-2" className="inline-block">
            <TitleChars text={TITLE_LINE_2} />
          </span>
        </h1>
        <p
          data-animate="desc"
          className="mt-4 max-w-sm text-base leading-relaxed text-amber-700 dark:text-amber-300"
          style={{ willChange: 'transform, opacity' }}
        >
          从灵感到发布，AI 全程陪伴你的内容创作之旅。微信公众号、种草笔记，一站搞定。
        </p>
        <div className="mt-6 flex flex-wrap gap-2">
          {featurePills.map((pill) => (
            <span
              key={pill.label}
              data-animate="pill"
              className="rounded-full border border-amber-300/60 bg-white/70 px-3.5 py-1.5 text-xs font-medium text-amber-800 backdrop-blur-sm dark:border-amber-600/40 dark:bg-amber-800/40 dark:text-amber-200"
              style={{ willChange: 'transform, opacity' }}
            >
              {pill.icon} {pill.label}
            </span>
          ))}
        </div>
      </div>

      {/* Form card */}
      <div
        data-animate="form-card"
        className="relative z-10 mx-4 w-full max-w-[380px] rounded-2xl bg-white p-8 shadow-2xl shadow-amber-900/10 dark:bg-amber-950 dark:shadow-black/30 sm:mx-0 xl:mr-24"
        style={{ willChange: 'transform, opacity' }}
      >
        {/* Mobile brand header */}
        <div data-animate="mobile-brand" className="mb-6 text-center xl:hidden">
          <div className="text-xs font-semibold tracking-widest text-amber-700 dark:text-amber-400">
            ✦ Anban 智能创作助手
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

      {/* ICP footer */}
      <div data-animate="footer" className="absolute bottom-4 w-full text-center text-xs text-amber-600/70 dark:text-amber-400/50">
        <p>© {new Date().getFullYear()} 成都北冕星辰科技有限公司</p>
        <a
          href="https://beian.miit.gov.cn/"
          target="_blank"
          rel="noreferrer"
          className="hover:text-amber-800 dark:hover:text-amber-300"
        >
          蜀ICP备2024071665号-3
        </a>
      </div>
    </div>
  )
}
