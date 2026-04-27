import { BookOpen, PenLine, FileImage } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { TaskType } from '@/types'

export const platformIcon: Record<TaskType, LucideIcon> = {
  rednote: BookOpen,
  article: PenLine,
  xls: FileImage,
}

export const platformIconColor: Record<TaskType, string> = {
  rednote: 'text-[#FF2442]',
  article: 'text-[#07C160]',
  xls: 'text-primary',
}

export function renderPlatformIcon(type: string) {
  const Icon = platformIcon[type as TaskType]
  if (!Icon) return null
  return <Icon className={platformIconColor[type as TaskType]} />
}
