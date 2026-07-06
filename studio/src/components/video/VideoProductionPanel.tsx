import { Check, Clapperboard, ImagePlus, RotateCcw, Scissors } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { VideoProductionArtifact, VideoProductionResponse } from '@/types'

const productionTabs = [
  { value: 'brief', label: 'Brief', artifact: 'creative-brief.md' },
  { value: 'anchors', label: '参考锚点', artifact: 'reference-anchors.md' },
  { value: 'shot-plan', label: '分镜计划', artifact: 'shot-plan.md' },
  { value: 'generation-plan', label: '生成计划', artifact: 'generation-plan.json' },
  { value: 'state', label: '连续性状态', artifact: 'project-state.json' },
  { value: 'take-log', label: 'Take Log', artifact: 'take-log.md' },
  { value: 'qc', label: 'QC', artifact: 'quality-review.md' },
  { value: 'delivery', label: '交付清单', artifact: 'delivery-manifest.json' },
]

const retakeLabels: Record<string, string> = {
  keep: 'Keep',
  fix_in_post: 'Fix in post',
  edit: 'Edit',
  re_roll: 'Re-roll',
  rewrite: 'Rewrite',
}

const nextActionLabels: Record<string, string> = {
  continue_editing: '继续剪辑',
  generate_cover: '生成封面',
  export_capcut_draft: '导出剪映草稿',
}

function artifactBody(artifact: VideoProductionArtifact | undefined) {
  if (!artifact || artifact.status === 'missing') return '未找到该产物'
  if (artifact.parsed_json) return JSON.stringify(artifact.parsed_json, null, 2)
  return artifact.content || '暂无可预览内容'
}

function ArtifactBody({ artifact }: { artifact: VideoProductionArtifact | undefined }) {
  return (
    <>
      {artifactBody(artifact).split('\n').map((line, index) => (
        <span key={`${line}-${index}`} className="block min-h-5">{line}</span>
      ))}
    </>
  )
}

function statusVariant(status: string | undefined) {
  if (status === 'available') return 'secondary' as const
  if (status === 'error') return 'destructive' as const
  return 'outline' as const
}

export function VideoProductionPanel({
  production,
  onRetakeAction,
  onNextAction,
  retakePending,
}: {
  production: VideoProductionResponse
  onRetakeAction: (action: string) => void
  onNextAction?: (action: string) => void
  retakePending?: boolean
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-xs text-muted-foreground">视频生产</p>
          <h2 className="mt-1 text-base font-semibold text-foreground">制作状态</h2>
        </div>
        <div className="flex flex-wrap gap-2">
          {production.scenario_key && <Badge variant="outline">{production.scenario_key}</Badge>}
          {production.production_mode && <Badge variant="outline">{production.production_mode}</Badge>}
        </div>
      </div>

      <Tabs defaultValue="brief" className="gap-3">
        <TabsList variant="line" className="max-w-full flex-wrap justify-start">
          {productionTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>{tab.label}</TabsTrigger>
          ))}
        </TabsList>
        {productionTabs.map((tab) => {
          const artifact = production.artifacts[tab.artifact]
          return (
            <TabsContent key={tab.value} value={tab.value} className="rounded-lg border border-border bg-muted/20 p-3">
              <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-medium text-foreground">{tab.artifact}</p>
                <Badge variant={statusVariant(artifact?.status)}>{artifact?.status || 'missing'}</Badge>
              </div>
              <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background px-3 py-2 text-xs leading-5 text-foreground">
                <ArtifactBody artifact={artifact} />
              </pre>
              {artifact?.error && <p className="mt-2 text-xs text-destructive">{artifact.error}</p>}
            </TabsContent>
          )
        })}
      </Tabs>

      {production.retake_actions.length > 0 && (
        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">返修决策</p>
          <div className="flex flex-wrap gap-2">
            {production.retake_actions.map((action) => (
              <Button key={action} type="button" variant="outline" size="sm" disabled={retakePending} onClick={() => onRetakeAction(action)}>
                {action === 'keep' ? <Check data-icon="inline-start" /> : <RotateCcw data-icon="inline-start" />}
                {retakeLabels[action] || action}
              </Button>
            ))}
          </div>
        </div>
      )}

      {production.next_actions.length > 0 && (
        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">交付后续</p>
          <div className="flex flex-wrap gap-2">
            {production.next_actions.map((action) => {
              const Icon = action === 'continue_editing' ? Scissors : action === 'generate_cover' ? ImagePlus : Clapperboard
              return (
                <Button key={action} type="button" variant="secondary" size="sm" onClick={() => onNextAction?.(action)}>
                  <Icon data-icon="inline-start" />
                  {nextActionLabels[action] || action}
                </Button>
              )
            })}
          </div>
        </div>
      )}

      <div className="grid gap-2 text-xs sm:grid-cols-3">
        <div className="rounded-md border border-border px-3 py-2">
          <p className="font-medium text-foreground">主体一致性</p>
          <p className="mt-1 text-muted-foreground">查看 QC 与 project-state。</p>
        </div>
        <div className="rounded-md border border-border px-3 py-2">
          <p className="font-medium text-foreground">产品保真 / CTA</p>
          <p className="mt-1 text-muted-foreground">查看 QC 与交付清单。</p>
        </div>
        <div className="rounded-md border border-border px-3 py-2">
          <p className="font-medium text-foreground">权利 / 元数据</p>
          <p className="mt-1 text-muted-foreground">确认交付前检查。</p>
        </div>
      </div>
    </div>
  )
}
