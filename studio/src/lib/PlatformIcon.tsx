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
      strokeWidth="2"
      viewBox="0 0 24 24"
      width="24"
    >
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <path d="M9 9.003a1 1 0 0 1 1.517-.859l4.997 2.997a1 1 0 0 1 0 1.718l-4.997 2.997A1 1 0 0 1 9 14.996z" />
      {platform === 'montage' ? (
        <g data-platform-symbol="sparkles" transform="translate(10 0) scale(.5)">
          <path d="M11.017 2.814a1 1 0 0 1 1.966 0l1.051 5.558a2 2 0 0 0 1.594 1.594l5.558 1.051a2 2 0 0 1 0 1.966l-5.558 1.051a2 2 0 0 0-1.594 1.594l-1.051 5.558a1 1 0 0 1-1.966 0l-1.051-5.558a2 2 0 0 0-1.594-1.594l-5.558-1.051a2 2 0 0 1 0-1.966l5.558-1.051a2 2 0 0 0 1.594-1.594z" />
          <path d="M20 2v4" />
          <path d="M22 4h-4" />
          <circle cx="4" cy="20" r="2" />
        </g>
      ) : (
        <g data-platform-symbol="copy" transform="translate(11 11) scale(.5)">
          <rect width="14" height="14" x="8" y="8" rx="2" />
          <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
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
