import { UserRound } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Switch } from '@/components/ui/switch'
import { supportsPortraitCover } from '@/lib/portrait-cover'
import type { Project } from '@/types'

interface PortraitCoverControlProps {
  type: string
  project?: Project
  checked: boolean
  coverEnabled?: boolean
  onCheckedChange: (checked: boolean) => void
}

export function PortraitCoverControl({ type, project, checked, coverEnabled = true, onCheckedChange }: PortraitCoverControlProps) {
  if (!supportsPortraitCover(type)) return null
  const portrait = project?.portrait_reference_image

  return (
    <div className="flex items-center gap-3 rounded-lg border border-border p-3">
      {portrait ? (
        <img src={portrait.download_url} alt="项目人物参考" className="h-10 w-10 shrink-0 rounded-md border object-cover" />
      ) : (
        <UserRound className="h-5 w-5 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-foreground">人物封面</p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {portrait ? '勾选后使用项目人物与标题设计封面' : <>项目尚未设置人物参考图 <Link className="text-primary hover:underline" to={`/projects?edit=${project?.id ?? ''}`}>去设置</Link></>}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">{!coverEnabled ? '已关闭封面生成' : checked ? '仅用于封面，保留人物身份并呈现标题' : '未勾选时生成普通封面，不使用项目人物'}</p>
      </div>
      <Switch
        aria-label="人物封面"
        checked={checked}
        disabled={!coverEnabled || (!portrait && !checked)}
        onCheckedChange={onCheckedChange}
      />
    </div>
  )
}
