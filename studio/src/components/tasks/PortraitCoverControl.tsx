import { UserRound } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import type { ReferenceImageSelection, ReferenceImageValue } from '@/types/asset'
import { supportsPortraitCover } from '@/lib/portrait-cover'
import type { Project } from '@/types'

interface PortraitCoverControlProps {
  type: string
  project?: Project
  value: ReferenceImageValue | null
  onValueChange: (value: ReferenceImageSelection | null) => void
  onUploadingChange: (uploading: boolean) => void
  checked: boolean
  coverEnabled?: boolean
  onCheckedChange: (checked: boolean) => void
}

export function PortraitCoverControl({ type, project, value, onValueChange, onUploadingChange, checked, coverEnabled = true, onCheckedChange }: PortraitCoverControlProps) {
  if (!supportsPortraitCover(type)) return null
  const projectPortrait = project?.portrait_reference_image
  const selectedAssetID = value && 'asset_id' in value ? value.asset_id : ''
  const displayValue = projectPortrait && selectedAssetID === projectPortrait.asset_id ? projectPortrait : value
  const isProjectDefault = Boolean(projectPortrait && selectedAssetID === projectPortrait.asset_id)
  const hasPortrait = Boolean(value)
  const handleValueChange = (nextValue: ReferenceImageSelection | null) => {
    onValueChange(nextValue)
    if (!nextValue && checked) onCheckedChange(false)
  }
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border p-3 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <UserRound className="mt-1 h-5 w-5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-foreground">人物参考</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{hasPortrait ? isProjectDefault ? '勾选后使用项目人物与标题设计封面' : '此任务单独使用所选人物参考' : projectPortrait ? '此任务不使用人物参考' : '项目尚未设置人物参考图'}</p>
          <div className="mt-2">
            <ReferenceAssetUpload
              value={displayValue}
              onChange={handleValueChange}
              purpose="project_portrait_reference"
              ariaLabel="人物参考图文件"
              imageAlt={isProjectDefault ? '项目人物参考' : '人物参考'}
              onUploadingChange={onUploadingChange}
            />
          </div>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 text-sm">
        <span>人物封面</span>
        <Switch
          aria-label="人物封面"
          checked={checked}
          disabled={!coverEnabled || !hasPortrait}
          onCheckedChange={onCheckedChange}
        />
      </div>
    </div>
  )
}
