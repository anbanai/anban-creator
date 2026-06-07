import { useRef, useCallback } from 'react'
import gsap from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

/**
 * Animates a number counter from 0 → target value.
 * Call with the element ref, target number, and format function.
 */
export function animateCounter(
  el: HTMLElement,
  target: number,
  format: (v: number) => string = (v) => String(Math.round(v))
) {
  const obj = { value: 0 }
  return gsap.to(obj, {
    value: target,
    duration: 1.6,
    ease: 'power2.out',
    delay: 0.4,
    onUpdate: () => {
      el.textContent = format(obj.value)
    },
  })
}

/**
 * Format percentage counter: 0 → "87%"
 */
export function formatPercent(v: number): string {
  return `${Math.round(v)}%`
}

/**
 * Format number with locale: 0 → "1,234"
 */
export function formatLocale(v: number): string {
  return Math.round(v).toLocaleString()
}

/**
 * Main dashboard entrance animation orchestrator.
 * Place on the root dashboard container. Uses data-animate attributes to target sections.
 *
 * Expected data-animate values in DOM:
 *   "header"      — PageHeader
 *   "stats"       — Stats cards grid (each child gets stagger)
 *   "credits"     — Credits card
 *   "invite"      — Invite card
 *   "charts"      — Charts grid (each child gets stagger)
 *   "recent"      — Recent tasks card
 *   "actions"     — Quick action cards (each child gets stagger)
 */
export function useDashboardAnimation() {
  const containerRef = useRef<HTMLDivElement>(null)

  useGSAP(() => {
    const tl = gsap.timeline({
      defaults: { ease: 'power3.out' },
    })

    // Header: fade + slide down
    tl.from('[data-animate="header"]', {
      y: -20,
      opacity: 0,
      duration: 0.6,
    })

    // Stats cards: stagger up from below with spring
    tl.from(
      '[data-animate="stats"] > *',
      {
        y: 40,
        opacity: 0,
        scale: 0.95,
        filter: 'blur(4px)',
        duration: 0.7,
        stagger: 0.1,
        ease: 'back.out(1.4)',
      },
      '-=0.3'
    )

    // Credits + Invite cards
    tl.from(
      '[data-animate="credits"], [data-animate="invite"]',
      {
        y: 30,
        opacity: 0,
        scale: 0.97,
        duration: 0.6,
        stagger: 0.12,
        ease: 'power3.out',
      },
      '-=0.4'
    )

    // Charts: fade in with stagger
    tl.from(
      '[data-animate="charts"] > *',
      {
        y: 20,
        opacity: 0,
        duration: 0.6,
        stagger: 0.15,
      },
      '-=0.3'
    )

    // Recent tasks
    tl.from(
      '[data-animate="recent"]',
      {
        y: 20,
        opacity: 0,
        duration: 0.5,
      },
      '-=0.3'
    )

    // Quick actions: bounce in
    tl.from(
      '[data-animate="actions"] > *',
      {
        y: 24,
        opacity: 0,
        scale: 0.92,
        duration: 0.6,
        stagger: 0.1,
        ease: 'back.out(1.7)',
      },
      '-=0.2'
    )
  }, { scope: containerRef })

  return containerRef
}
