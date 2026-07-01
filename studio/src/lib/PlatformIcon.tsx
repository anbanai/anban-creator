import { BookOpen, ShoppingBag, Signature, Video } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { TaskType } from '@/types'

export const platformIcon: Record<TaskType, LucideIcon> = {
  seednote: BookOpen,
  article: Signature,
  ecommerce: ShoppingBag,
  video: Video,
}

export const platformIconColor: Record<TaskType, string> = {
  seednote: 'text-[#FF2442]',
  article: 'text-[#07C160]',
  ecommerce: 'text-[#FF6A00]',
  video: 'text-[#2563EB]',
}

export const platformBorderColor: Record<string, string> = {
  article: 'border-l-[#07C160]',
  seednote: 'border-l-[#FF2442]',
  ecommerce: 'border-l-[#FF6A00]',
  video: 'border-l-[#2563EB]',
}

export const platformHoverBorderColor: Record<string, string> = {
  article: 'hover:border-l-[#07C160]/50',
  seednote: 'hover:border-l-[#FF2442]/50',
  ecommerce: 'hover:border-l-[#FF6A00]/50',
  video: 'hover:border-l-[#2563EB]/50',
}

export const platformBadgeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  article: 'secondary',
  seednote: 'destructive',
  ecommerce: 'default',
  video: 'outline',
}

export const platformBgColor: Record<string, string> = {
  article: 'bg-[#07C160]/10',
  seednote: 'bg-[#FF2442]/10',
  ecommerce: 'bg-[#FF6A00]/10',
  video: 'bg-[#2563EB]/10',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type as TaskType]
  if (!Icon) return null
  return <Icon className={platformIconColor[type as TaskType]} />
}
