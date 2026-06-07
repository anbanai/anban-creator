import { BookOpen, Signature, FileText } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { TaskType } from '@/types'

export const platformIcon: Record<TaskType, LucideIcon> = {
  seednote: BookOpen,
  rednote: BookOpen,
  article: Signature,
  xls: FileText,
}

export const platformIconColor: Record<TaskType, string> = {
  seednote: 'text-[#FF2442]',
  rednote: 'text-[#FF2442]',
  article: 'text-[#07C160]',
  xls: 'text-blue-500',
}

export const platformBorderColor: Record<string, string> = {
  article: 'border-l-[#07C160]',
  seednote: 'border-l-[#FF2442]',
  rednote: 'border-l-[#FF2442]',
  xls: 'border-l-blue-500',
}

export const platformHoverBorderColor: Record<string, string> = {
  article: 'hover:border-l-[#07C160]/50',
  seednote: 'hover:border-l-[#FF2442]/50',
  rednote: 'hover:border-l-[#FF2442]/50',
  xls: 'hover:border-l-blue-500/50',
}

export const platformBadgeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  article: 'secondary',
  seednote: 'destructive',
  rednote: 'destructive',
  xls: 'default',
}

export const platformBgColor: Record<string, string> = {
  article: 'bg-[#07C160]/10',
  seednote: 'bg-[#FF2442]/10',
  rednote: 'bg-[#FF2442]/10',
  xls: 'bg-blue-500/10',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type as TaskType]
  if (!Icon) return null
  return <Icon className={platformIconColor[type as TaskType]} />
}
