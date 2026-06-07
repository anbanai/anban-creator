import { useRef, useCallback } from 'react'
import gsap from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

// Compact 2D simplex noise
function createNoise() {
  const perm = new Uint8Array(512)
  const p = new Uint8Array(256)
  for (let i = 0; i < 256; i++) p[i] = i
  for (let i = 255; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [p[i], p[j]] = [p[j], p[i]]
  }
  for (let i = 0; i < 512; i++) perm[i] = p[i & 255]

  const grad = [[1,1],[-1,1],[1,-1],[-1,-1],[1,0],[-1,0],[0,1],[0,-1]]

  return function noise2D(x: number, y: number): number {
    const F2 = 0.5 * (Math.sqrt(3) - 1)
    const G2 = (3 - Math.sqrt(3)) / 6
    const s = (x + y) * F2
    const i = Math.floor(x + s)
    const j = Math.floor(y + s)
    const t = (i + j) * G2
    const x0 = x - (i - t)
    const y0 = y - (j - t)
    const i1 = x0 > y0 ? 1 : 0
    const j1 = x0 > y0 ? 0 : 1
    const x1 = x0 - i1 + G2
    const y1 = y0 - j1 + G2
    const x2 = x0 - 1 + 2 * G2
    const y2 = y0 - 1 + 2 * G2
    const ii = i & 255
    const jj = j & 255

    let n0 = 0, n1 = 0, n2 = 0
    let t0 = 0.5 - x0 * x0 - y0 * y0
    if (t0 >= 0) {
      const g = perm[ii + perm[jj]] & 7
      t0 *= t0
      n0 = t0 * t0 * (grad[g][0] * x0 + grad[g][1] * y0)
    }
    let t1 = 0.5 - x1 * x1 - y1 * y1
    if (t1 >= 0) {
      const g = perm[ii + i1 + perm[jj + j1]] & 7
      t1 *= t1
      n1 = t1 * t1 * (grad[g][0] * x1 + grad[g][1] * y1)
    }
    let t2 = 0.5 - x2 * x2 - y2 * y2
    if (t2 >= 0) {
      const g = perm[ii + 1 + perm[jj + 1]] & 7
      t2 *= t2
      n2 = t2 * t2 * (grad[g][0] * x2 + grad[g][1] * y2)
    }
    return 70 * (n0 + n1 + n2)
  }
}

// Color palette — warm amber/gold tones matching the site's OKLCH primary
const COLORS = [
  [0.72, 0.17, 60],  // primary warm gold
  [0.65, 0.15, 30],  // warm orange
  [0.55, 0.12, 170], // teal accent
  [0.60, 0.10, 330], // rose accent
  [0.50, 0.08, 250], // soft blue
  [0.67, 0.17, 60],  // bright gold
]

function oklchToRgb(l: number, c: number, h: number): [number, number, number] {
  // Simplified OKLCH → sRGB approximation
  const a = c * Math.cos((h * Math.PI) / 180)
  const b = c * Math.sin((h * Math.PI) / 180)
  const L_ = l + 0.3963377774 * a + 0.2158037573 * b
  const M_ = l - 0.1055613458 * a - 0.0638541728 * b
  const S_ = l - 0.0894841775 * a - 1.2914855480 * b
  const l_ = L_ * L_ * L_
  const m_ = M_ * M_ * M_
  const s_ = S_ * S_ * S_
  const r = +4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_
  const g = -1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_
  const bl = -0.0041960863 * l_ - 0.7034186147 * m_ + 1.7076147010 * s_
  return [
    Math.max(0, Math.min(255, Math.round(r * 255))),
    Math.max(0, Math.min(255, Math.round(g * 255))),
    Math.max(0, Math.min(255, Math.round(bl * 255))),
  ]
}

// Pre-compute palette as RGB arrays
const PALETTE = COLORS.map(([l, c, h]) => oklchToRgb(l, c, h))

function lerpColor(a: number[], b: number[], t: number): [number, number, number] {
  return [
    Math.round(a[0] + (b[0] - a[0]) * t),
    Math.round(a[1] + (b[1] - a[1]) * t),
    Math.round(a[2] + (b[2] - a[2]) * t),
  ]
}

export default function GeneratingAnimation() {
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rafRef = useRef<number>(0)
  const timeRef = useRef({ value: 0, speed: 1 })
  const noiseRef = useRef<ReturnType<typeof createNoise> | null>(null)
  const offscreenRef = useRef<{ canvas: HTMLCanvasElement; sw: number; sh: number } | null>(null)

  const render = useCallback(() => {
    const canvas = canvasRef.current
    if (!canvas || !noiseRef.current) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const w = canvas.width
    const h = canvas.height
    const noise = noiseRef.current
    const t = timeRef.current.value

    // Low-res render for performance, then upscale
    const scale = 4
    const sw = Math.ceil(w / scale)
    const sh = Math.ceil(h / scale)
    const imageData = ctx.createImageData(sw, sh)
    const data = imageData.data

    for (let y = 0; y < sh; y++) {
      for (let x = 0; x < sw; x++) {
        const nx = x / sw
        const ny = y / sh

        // 3 octaves of noise with flowing offsets
        const n1 = noise(nx * 3 + t * 0.4, ny * 3 + t * 0.3)
        const n2 = noise(nx * 5 - t * 0.25, ny * 5 + t * 0.35) * 0.5
        const n3 = noise(nx * 8 + t * 0.15, ny * 8 - t * 0.2) * 0.25

        // Combined noise → clamped [0, 1] for palette mapping
        const v = Math.max(0, Math.min(1, (n1 + n2 + n3) * 0.4 + 0.5))

        // Map to palette with smooth interpolation
        const palIdx = v * (PALETTE.length - 1)
        const idx = Math.floor(palIdx)
        const frac = palIdx - idx
        const c1 = PALETTE[Math.min(idx, PALETTE.length - 1)]
        const c2 = PALETTE[Math.min(idx + 1, PALETTE.length - 1)]
        const [r, g, b] = lerpColor(c1, c2, frac)

        // Add subtle luminance variation
        const lum = 0.85 + n1 * 0.15

        const i = (y * sw + x) * 4
        data[i] = Math.min(255, Math.round(r * lum))
        data[i + 1] = Math.min(255, Math.round(g * lum))
        data[i + 2] = Math.min(255, Math.round(b * lum))
        data[i + 3] = 255
      }
    }

    // Draw at low res then scale up (creates natural blur/smoothness)
    if (!offscreenRef.current || offscreenRef.current.sw !== sw || offscreenRef.current.sh !== sh) {
      offscreenRef.current = { canvas: document.createElement('canvas'), sw, sh }
      offscreenRef.current.canvas.width = sw
      offscreenRef.current.canvas.height = sh
    }
    const offscreen = offscreenRef.current.canvas
    offscreen.getContext('2d')!.putImageData(imageData, 0, 0)

    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(offscreen, 0, 0, w, h)

    // Advance time
    timeRef.current.value += 0.008 * timeRef.current.speed

    rafRef.current = requestAnimationFrame(render)
  }, [])

  useGSAP(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    // Size canvas to container
    const resizeCanvas = () => {
      const parent = canvas.parentElement
      if (!parent) return
      const rect = parent.getBoundingClientRect()
      const dpr = Math.min(window.devicePixelRatio, 1.5)
      canvas.width = Math.round(rect.width * dpr)
      canvas.height = Math.round(rect.height * dpr)
      canvas.style.width = `${rect.width}px`
      canvas.style.height = `${rect.height}px`
    }
    resizeCanvas()
    window.addEventListener('resize', resizeCanvas)

    noiseRef.current = createNoise()
    rafRef.current = requestAnimationFrame(render)

    // GSAP drives the speed pulsing — slow → fast → slow breathing
    gsap.to(timeRef.current, {
      speed: 2.5,
      duration: 3,
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
    })

    // Pulse the overlay text
    gsap.to('.gen-fluid-text', {
      opacity: 0.4,
      duration: 2,
      ease: 'sine.inOut',
      repeat: -1,
      yoyo: true,
    })

    return () => {
      cancelAnimationFrame(rafRef.current)
      window.removeEventListener('resize', resizeCanvas)
    }
  }, { scope: containerRef })

  return (
    <div ref={containerRef} className="relative flex h-full flex-col items-center justify-center">
      <canvas
        ref={canvasRef}
        className="absolute inset-0 h-full w-full"
        style={{ filter: 'blur(1px) saturate(1.3)', opacity: 0.6 }}
      />
      {/* Radial vignette overlay */}
      <div
        className="pointer-events-none absolute inset-0"
        style={{ background: 'radial-gradient(ellipse at center, transparent 20%, var(--color-background) 75%)' }}
      />
      <div className="gen-fluid-text relative z-10 text-sm font-medium text-muted-foreground">
        正在生成图片...
      </div>
    </div>
  )
}
