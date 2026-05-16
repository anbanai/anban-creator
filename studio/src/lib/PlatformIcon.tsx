import { BookOpen, Signature } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { TaskType } from '@/types'

export const platformIcon: Record<TaskType, LucideIcon> = {
  seednote: BookOpen,
  article: Signature,
}

export const platformIconColor: Record<TaskType, string> = {
  seednote: 'text-[#FF2442]',
  article: 'text-[#07C160]',
}

export const platformBorderColor: Record<string, string> = {
  article: 'border-l-[#07C160]',
  seednote: 'border-l-[#FF2442]',
}

export const platformHoverBorderColor: Record<string, string> = {
  article: 'hover:border-l-[#07C160]/50',
  seednote: 'hover:border-l-[#FF2442]/50',
}

export const platformBadgeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  article: 'secondary',
  seednote: 'destructive',
}

export const platformBgColor: Record<string, string> = {
  article: 'bg-[#07C160]/10',
  seednote: 'bg-[#FF2442]/10',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type as TaskType]
  if (!Icon) return null
  return <Icon className={platformIconColor[type as TaskType]} />
}
