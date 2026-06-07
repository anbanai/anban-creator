import { useRef } from 'react'
import gsap from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

export default function GeneratingAnimation() {
  const containerRef = useRef<HTMLDivElement>(null)

  useGSAP(() => {
    const tl = gsap.timeline({ repeat: -1 })

    // Core orb pulsing
    tl.to('.gen-orb', {
      scale: 1.15,
      duration: 1.2,
      ease: 'power2.inOut',
      yoyo: true,
      repeat: 1,
    }, 0)

    // Outer ring rotation
    gsap.to('.gen-ring', {
      rotation: 360,
      duration: 8,
      ease: 'none',
      repeat: -1,
    })

    // Second ring counter-rotation
    gsap.to('.gen-ring-2', {
      rotation: -360,
      duration: 12,
      ease: 'none',
      repeat: -1,
    })

    // Orbiting dots
    gsap.to('.gen-dot', {
      rotation: 360,
      duration: 6,
      ease: 'none',
      repeat: -1,
      stagger: {
        each: 1.5,
        from: 'random',
      },
    })

    // Floating particles
    gsap.to('.gen-particle', {
      y: 'random(-30, 30)',
      x: 'random(-20, 20)',
      opacity: 'random(0.2, 0.8)',
      scale: 'random(0.5, 1.5)',
      duration: 'random(1.5, 3)',
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
      stagger: { each: 0.15, from: 'random' },
    })

    // Scan line
    tl.to('.gen-scanline', {
      yPercent: 200,
      duration: 2,
      ease: 'power1.inOut',
      repeat: -1,
      yoyo: true,
    }, 0)

    // Pulse text opacity
    gsap.to('.gen-text', {
      opacity: 0.4,
      duration: 1.5,
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
    })

    return () => {
      tl.kill()
    }
  }, { scope: containerRef })

  return (
    <div ref={containerRef} className="flex h-full flex-col items-center justify-center">
      <div className="gen-scene relative flex h-48 w-48 items-center justify-center">
        {/* Outer ring 2 */}
        <div className="gen-ring-2 absolute inset-2 rounded-full border border-primary/10" style={{ borderTopColor: 'transparent', borderRightColor: 'transparent' }} />

        {/* Outer ring */}
        <div className="gen-ring absolute inset-4 rounded-full border border-primary/20" style={{ borderBottomColor: 'transparent', borderLeftColor: 'transparent' }} />

        {/* Orbiting dots */}
        {[0, 1, 2, 3].map((i) => (
          <div
            key={i}
            className="gen-dot absolute"
            style={{ top: '50%', left: '50%', marginLeft: -4, marginTop: -4 }}
          >
            <div
              className="h-2 w-2 rounded-full bg-primary/60"
              style={{ transform: `translateX(${56}px)` }}
            />
          </div>
        ))}

        {/* Core orb */}
        <div className="gen-orb relative flex h-24 w-24 items-center justify-center rounded-full bg-primary/5">
          {/* Inner glow */}
          <div className="absolute inset-0 rounded-full bg-gradient-to-br from-primary/20 to-primary/5" />

          {/* Scan line */}
          <div className="gen-scanline absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/60 to-transparent" />

          {/* AI icon - abstract grid */}
          <svg width="32" height="32" viewBox="0 0 32 32" fill="none" className="relative z-10 text-primary/60">
            <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
            <rect x="18" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
            <rect x="2" y="18" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
            <rect x="18" y="18" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" fill="currentColor" fillOpacity="0.15" />
            <circle cx="8" cy="8" r="2" fill="currentColor" fillOpacity="0.5" />
            <circle cx="24" cy="8" r="2" fill="currentColor" fillOpacity="0.5" />
            <circle cx="8" cy="24" r="2" fill="currentColor" fillOpacity="0.5" />
            <circle cx="24" cy="24" r="2" fill="currentColor" fillOpacity="0.8" />
          </svg>
        </div>

        {/* Floating particles */}
        {Array.from({ length: 12 }).map((_, i) => (
          <div
            key={`p-${i}`}
            className="gen-particle absolute h-1 w-1 rounded-full bg-primary/40"
            style={{
              top: `${15 + Math.random() * 70}%`,
              left: `${15 + Math.random() * 70}%`,
              opacity: 0.3 + Math.random() * 0.5,
            }}
          />
        ))}
      </div>

      <div className="gen-text mt-6 text-sm font-medium text-muted-foreground">
        正在生成图片...
      </div>
    </div>
  )
}
