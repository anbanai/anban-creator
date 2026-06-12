import { useRef } from 'react'
import gsap from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

export default function GeneratingAnimation() {
  const containerRef = useRef<HTMLDivElement>(null)

  useGSAP(() => {
    // Floating orbs — slow organic drift
    const orbs = containerRef.current?.querySelectorAll('.gen-orb')
    orbs?.forEach((orb, i) => {
      gsap.to(orb, {
        x: `random(-40, 40)`,
        y: `random(-30, 30)`,
        scale: `random(0.85, 1.2)`,
        opacity: `random(0.25, 0.55)`,
        duration: 7 + i * 2.5,
        ease: 'sine.inOut',
        repeat: -1,
        yoyo: true,
        delay: i * 0.8,
      })
    })

    // Skeleton shimmer sweep
    const shimmers = containerRef.current?.querySelectorAll('.gen-shimmer')
    shimmers?.forEach((shimmer) => {
      gsap.fromTo(shimmer,
        { xPercent: -100 },
        { xPercent: 200, duration: 1.8, ease: 'power2.inOut', repeat: -1, repeatDelay: 1 },
      )
    })

    // Stagger skeleton cards entrance
    const cards = containerRef.current?.querySelectorAll('.gen-skeleton')
    gsap.from(cards ?? [], {
      opacity: 0,
      scale: 0.92,
      y: 12,
      duration: 0.5,
      stagger: 0.07,
      ease: 'power2.out',
    })

    // Pulse loading text
    gsap.to('.gen-loading-text', {
      opacity: 0.4,
      duration: 2.2,
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
    })

    // Subtle cyclic progress bar
    const bars = containerRef.current?.querySelectorAll('.gen-progress')
    bars?.forEach((bar) => {
      gsap.fromTo(bar,
        { scaleX: 0 },
        { scaleX: 1, duration: 3.5, ease: 'power1.inOut', repeat: -1, repeatDelay: 0.5 },
      )
    })
  }, { scope: containerRef })

  return (
    <div ref={containerRef} className="relative flex h-full flex-col items-center justify-center overflow-hidden">
      {/* Floating gradient orbs */}
      <div className="pointer-events-none absolute inset-0">
        <div className="gen-orb absolute left-[20%] top-[25%] h-36 w-36 rounded-full bg-primary/20 blur-[70px]" />
        <div className="gen-orb absolute right-[22%] top-[40%] h-44 w-44 rounded-full bg-chart-2/15 blur-[80px]" />
        <div className="gen-orb absolute left-[45%] bottom-[20%] h-32 w-32 rounded-full bg-chart-4/15 blur-[60px]" />
      </div>

      {/* Skeleton card grid */}
      <div className="relative grid w-full max-w-md grid-cols-3 gap-3 px-8">
        {Array.from({ length: 6 }).map((_, i) => (
          <div
            key={i}
            className="gen-skeleton relative aspect-square overflow-hidden rounded-xl bg-muted/25 ring-1 ring-border/15"
          >
            <div className="gen-shimmer absolute inset-0 bg-gradient-to-r from-transparent via-white/8 to-transparent" />
            <div className="gen-progress absolute bottom-0 left-0 h-[2px] origin-left bg-primary/30" />
          </div>
        ))}
      </div>

      {/* Loading text */}
      <p className="gen-loading-text mt-6 text-sm font-medium text-muted-foreground">
        正在生成图片...
      </p>
    </div>
  )
}
