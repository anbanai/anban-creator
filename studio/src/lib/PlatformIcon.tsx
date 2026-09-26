import type { ComponentType } from 'react'
import { useCallback } from 'react'
import { BookOpen, MessageCircle, ShoppingBag, Signature } from 'lucide-react'
import { motion, useAnimation, useReducedMotion, type Variants } from 'motion/react'
import type { TaskType } from '@/types'

type PlatformIconComponent = ComponentType<{ className?: string }>

const clapperLidVariants: Variants = {
  normal: { rotate: 0, originX: '2.6px', originY: '8.6px' },
  animate: {
    rotate: [0, -15, 5, 0],
    transition: { duration: 0.5, velocity: 0.3 },
  },
}

const clapperBodyVariants: Variants = {
  normal: { scale: 1, originX: '12px', originY: '21px' },
  animate: {
    scale: [1, 1.04, 1],
    transition: { duration: 0.4, velocity: 0.3 },
  },
}

const copySheetVariants: Variants = {
  normal: { x: 0, y: 0, scale: 1, originX: '8px', originY: '8px' },
  animate: {
    x: [0, 2.4, -0.8, 0],
    y: [0, -2.4, 0.8, 0],
    scale: [1, 0.96, 1.02, 1],
    transition: { duration: 0.55, velocity: 0.3 },
  },
}

const copyFrontVariants: Variants = {
  normal: { scale: 1, originX: '2px', originY: '16px' },
  animate: {
    scale: [1, 1.05, 1],
    transition: { duration: 0.4, velocity: 0.3 },
  },
}

function VideoPlatformIcon({ platform, className }: { platform: 'montage' | 'hypit'; className?: string }) {
  const controls = useAnimation()
  const reducedMotion = useReducedMotion()

  const handleMouseEnter = useCallback(() => {
    if (!reducedMotion) controls.start('animate')
  }, [controls, reducedMotion])

  const handleMouseLeave = useCallback(() => {
    if (!reducedMotion) controls.start('normal')
  }, [controls, reducedMotion])

  return (
    <svg
      aria-hidden="true"
      className={`shrink-0 ${className ?? ''}`}
      data-platform-icon={platform}
      fill="none"
      focusable="false"
      height="24"
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
      viewBox="0 0 24 24"
      width="24"
    >
      {platform === 'montage' ? (
        <g data-platform-symbol="generate">
          <path d="m12.296 3.464 3.02 3.956" />
          <motion.path
            animate={controls}
            d="M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4c1.1-.3 2.2.3 2.5 1.3z"
            variants={clapperLidVariants}
          />
          <motion.path
            animate={controls}
            d="M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"
            variants={clapperBodyVariants}
          />
          <path d="m6.18 5.276 3.1 3.899" />
        </g>
      ) : (
        <g data-platform-symbol="replicate">
          <motion.rect
            animate={controls}
            height="14"
            rx="2"
            ry="2"
            variants={copySheetVariants}
            width="14"
            x="8"
            y="8"
          />
          <motion.path
            animate={controls}
            d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"
            variants={copyFrontVariants}
          />
        </g>
      )}
    </svg>
  )
}

function VideoGenerationIcon(props: { className?: string }) {
  return <VideoPlatformIcon platform="montage" {...props} />
}

function VideoReplicationIcon(props: { className?: string }) {
  return <VideoPlatformIcon platform="hypit" {...props} />
}

export const platformIcon: Record<TaskType, PlatformIconComponent> = {
  seednote: BookOpen,
  article: Signature,
  moments: MessageCircle,
  ecommerce: ShoppingBag,
  viral_analysis: BookOpen,
  montage: VideoGenerationIcon,
  hypit: VideoReplicationIcon,
}

export const platformIconColor: Record<TaskType, string> = {
  seednote: 'text-[#FF2442]',
  article: 'text-[#07C160]',
  moments: 'text-[#2F855A]',
  ecommerce: 'text-[#FF6A00]',
  viral_analysis: 'text-[#7C3AED]',
  montage: 'text-[#9333EA]',
  hypit: 'text-[#F97316]',
}

export const platformBorderColor: Record<string, string> = {
  article: 'border-l-[#07C160]',
  seednote: 'border-l-[#FF2442]',
  moments: 'border-l-[#2F855A]',
  ecommerce: 'border-l-[#FF6A00]',
  viral_analysis: 'border-l-[#7C3AED]',
  montage: 'border-l-[#9333EA]',
  hypit: 'border-l-[#F97316]',
}

export const platformHoverBorderColor: Record<string, string> = {
  article: 'hover:border-l-[#07C160]/50',
  seednote: 'hover:border-l-[#FF2442]/50',
  moments: 'hover:border-l-[#2F855A]/50',
  ecommerce: 'hover:border-l-[#FF6A00]/50',
  viral_analysis: 'hover:border-l-[#7C3AED]/50',
  montage: 'hover:border-l-[#9333EA]/50',
  hypit: 'hover:border-l-[#F97316]/50',
}

export const platformBadgeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  article: 'secondary',
  seednote: 'destructive',
  moments: 'secondary',
  ecommerce: 'default',
  viral_analysis: 'outline',
  montage: 'outline',
  hypit: 'outline',
}

// Explicit platform accents stay independent of the application's primary theme.
export const platformBadgeClassName: Record<string, string> = {
  montage: 'border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-800 dark:bg-purple-950 dark:text-purple-300',
  hypit: 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950 dark:text-orange-300',
}

export const platformBgColor: Record<string, string> = {
  article: 'bg-[#07C160]/10',
  seednote: 'bg-[#FF2442]/10',
  moments: 'bg-[#2F855A]/10',
  ecommerce: 'bg-[#FF6A00]/10',
  viral_analysis: 'bg-[#7C3AED]/10',
  montage: 'bg-[#9333EA]/10',
  hypit: 'bg-[#F97316]/10',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type as TaskType]
  if (!Icon) return null
  return <Icon className={platformIconColor[type as TaskType]} />
}
