import type { ComponentType } from 'react'
import { useCallback } from 'react'
import { BookOpen, Flame, Images, MessageCircle, ShoppingBag, Signature, PenLine } from 'lucide-react'
import { motion, useAnimation, useReducedMotion, type Variants } from 'motion/react'

type PlatformIconComponent = ComponentType<{ className?: string }>

const generateSweepVariants: Variants = {
  normal: { x: 0, opacity: 0 },
  animate: {
    x: [0, 9],
    opacity: [0, 0.16, 0.16, 0],
    transition: { duration: 0.7, times: [0, 0.25, 0.65, 1], ease: 'easeInOut' },
  },
}

const generatePlayVariants: Variants = {
  normal: { scale: 1, originX: '12px', originY: '12.5px' },
  animate: {
    scale: [1, 0.88, 1.08, 1],
    transition: { duration: 0.5, velocity: 0.3 },
  },
}

const replicateEchoVariants: Variants = {
  normal: { scale: 1, x: 0, y: 0, opacity: 0.4, originX: '13px', originY: '10.5px' },
  animate: {
    scale: [1, 1.16, 0.97, 1],
    x: [0, -1.6, 0.7, 0],
    y: [0, -1.2, 0.5, 0],
    opacity: [0.4, 0.95, 0.5, 0.4],
    transition: { duration: 0.6, velocity: 0.3 },
  },
}

const replicateFrameVariants: Variants = {
  normal: { scale: 1, originX: '10.5px', originY: '13.5px' },
  animate: {
    scale: [1, 0.95, 1.05, 1],
    transition: { duration: 0.45, velocity: 0.3 },
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
      strokeWidth="1.75"
      viewBox="0 0 24 24"
      width="24"
    >
      {platform === 'montage' ? (
        <g data-platform-symbol="generate">
          <rect height="18" rx="5" width="18" x="3" y="3" />
          <motion.path
            animate={controls}
            d="M10.2 8.7v6.6a.55.55 0 0 0 .84.47l5.3-3.3a.55.55 0 0 0 0-.94l-5.3-3.3a.55.55 0 0 0-.84.47Z"
            fill="currentColor"
            stroke="none"
            variants={generatePlayVariants}
          />
          <motion.rect
            animate={controls}
            fill="currentColor"
            height="14"
            opacity="0"
            rx="1.75"
            stroke="none"
            variants={generateSweepVariants}
            width="3.5"
            x="6"
            y="5"
          />
        </g>
      ) : (
        <g data-platform-symbol="replicate">
          <motion.rect
            animate={controls}
            height="15"
            opacity="0.4"
            rx="4.25"
            variants={replicateEchoVariants}
            width="15"
            x="5.5"
            y="3"
          />
          <motion.rect
            animate={controls}
            height="15"
            rx="4.25"
            variants={replicateFrameVariants}
            width="15"
            x="3"
            y="6"
          />
          <path
            d="M9.2 11.2v4.6a.5.5 0 0 0 .76.43l3.9-2.3a.5.5 0 0 0 0-.86l-3.9-2.3a.5.5 0 0 0-.76.43Z"
            fill="currentColor"
            stroke="none"
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

// Projects use the platform key `wechat`, which keeps the shared WeChat
// identity, while tasks use the more specific `wechat-article` (long-form
// writing) and `wechat-picture` (multi-image) task types with distinct glyphs.
export const platformIcon: Record<string, PlatformIconComponent> = {
  seednote: BookOpen,
  wechat: Signature,
  'wechat-article': Signature,
  'wechat-picture': Images,
  moments: MessageCircle,
  ecommerce: ShoppingBag,
  viral_analysis: Flame,
  profile_analysis: BookOpen,
  montage: VideoGenerationIcon,
  'whiteboard-animation': PenLine,
  hypit: VideoReplicationIcon,
}

export const platformIconColor: Record<string, string> = {
  seednote: 'text-[#FF2442]',
  wechat: 'text-[#07C160]',
  'wechat-article': 'text-[#07C160]',
  'wechat-picture': 'text-[#07C160]',
  moments: 'text-[#2F855A]',
  ecommerce: 'text-[#FF6A00]',
  viral_analysis: 'text-[#7C3AED]',
  profile_analysis: 'text-[#64748B]',
  montage: 'text-[#9333EA]',
  'whiteboard-animation': 'text-[#0F766E]',
  hypit: 'text-[#F97316]',
}

export const platformBorderColor: Record<string, string> = {
  wechat: 'border-l-[#07C160]',
  'wechat-article': 'border-l-[#07C160]',
  'wechat-picture': 'border-l-[#07C160]',
  seednote: 'border-l-[#FF2442]',
  moments: 'border-l-[#2F855A]',
  ecommerce: 'border-l-[#FF6A00]',
  viral_analysis: 'border-l-[#7C3AED]',
  montage: 'border-l-[#9333EA]',
  'whiteboard-animation': 'border-l-[#0F766E]',
  hypit: 'border-l-[#F97316]',
}

export const platformHoverBorderColor: Record<string, string> = {
  wechat: 'hover:border-l-[#07C160]/50',
  'wechat-article': 'hover:border-l-[#07C160]/50',
  'wechat-picture': 'hover:border-l-[#07C160]/50',
  seednote: 'hover:border-l-[#FF2442]/50',
  moments: 'hover:border-l-[#2F855A]/50',
  ecommerce: 'hover:border-l-[#FF6A00]/50',
  viral_analysis: 'hover:border-l-[#7C3AED]/50',
  montage: 'hover:border-l-[#9333EA]/50',
  'whiteboard-animation': 'hover:border-l-[#0F766E]/50',
  hypit: 'hover:border-l-[#F97316]/50',
}

export const platformBadgeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  'wechat-article': 'secondary',
  'wechat-picture': 'secondary',
  seednote: 'destructive',
  moments: 'secondary',
  ecommerce: 'default',
  viral_analysis: 'outline',
  montage: 'outline',
  'whiteboard-animation': 'outline',
  hypit: 'outline',
}

// Explicit platform accents stay independent of the application's primary theme.
export const platformBadgeClassName: Record<string, string> = {
  montage: 'border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-800 dark:bg-purple-950 dark:text-purple-300',
  'whiteboard-animation': 'border-teal-200 bg-teal-50 text-teal-700 dark:border-teal-800 dark:bg-teal-950 dark:text-teal-300',
  hypit: 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950 dark:text-orange-300',
}

export const platformBgColor: Record<string, string> = {
  wechat: 'bg-[#07C160]/10',
  'wechat-article': 'bg-[#07C160]/10',
  'wechat-picture': 'bg-[#07C160]/10',
  seednote: 'bg-[#FF2442]/10',
  moments: 'bg-[#2F855A]/10',
  ecommerce: 'bg-[#FF6A00]/10',
  viral_analysis: 'bg-[#7C3AED]/10',
  montage: 'bg-[#9333EA]/10',
  'whiteboard-animation': 'bg-[#0F766E]/10',
  hypit: 'bg-[#F97316]/10',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type]
  if (!Icon) return null
  return <Icon className={platformIconColor[type]} />
}
