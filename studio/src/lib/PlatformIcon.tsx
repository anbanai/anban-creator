import type { ComponentType } from 'react'
import { BookOpen, MessageCircle, ShoppingBag, Signature } from 'lucide-react'
import type { TaskType } from '@/types'

type PlatformIconComponent = ComponentType<{ className?: string }>

function VideoPlatformIcon({ platform, className }: { platform: 'montage' | 'hypit'; className?: string }) {
  return (
    <svg
      aria-hidden="true"
      className={`shrink-0 ${className ?? ''}`}
      data-platform-icon={platform}
      fill="none"
      focusable="false"
      height="24"
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
          <path
            d="M10.2 8.7v6.6a.55.55 0 0 0 .84.47l5.3-3.3a.55.55 0 0 0 0-.94l-5.3-3.3a.55.55 0 0 0-.84.47Z"
            fill="currentColor"
            stroke="none"
          />
          <rect
            className="animate-platform-generate"
            fill="currentColor"
            height="14"
            opacity="0"
            rx="1.75"
            stroke="none"
            width="3.5"
            x="6"
            y="5"
          />
        </g>
      ) : (
        <g data-platform-symbol="replicate">
          <rect
            className="animate-platform-replicate"
            height="15"
            opacity="0.4"
            rx="4.25"
            width="15"
            x="5.5"
            y="3"
          />
          <rect height="15" rx="4.25" width="15" x="3" y="6" />
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
