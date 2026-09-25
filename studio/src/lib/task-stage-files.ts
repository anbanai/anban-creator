import type { TaskFile, TaskLifecycle, TaskLifecycleStage, WorkflowStatus } from '@/types'
import { parseWorkflowStatus } from '@/lib/workflow-readiness'

const roleStages: Record<string, RegExp> = {
  topic: /选题|研究|调研|research|topic/i,
  outline: /大纲|结构规划|outline/i,
  draft: /初稿|撰写|写作|正文|writing|draft/i,
  markdown: /撰写|写作|正文|文案|writing|copywriting/i,
  content: /撰写|写作|正文|文案|writing|copywriting/i,
  copywriting: /文案|写作|copywriting/i,
  final_markdown: /定稿|正文|润色|写作|writing|polish/i,
  html: /排版|格式转换|format|layout/i,
  draft_package: /排版|交付|打包|package|delivery/i,
  image: /配图|图片|视觉|出图|image|visual/i,
  image_manifest: /配图|图片|视觉|image|visual/i,
  cover: /封面|cover/i,
  review: /审核|复核|复盘|验收|质检|review|quality/i,
  video: /渲染|剪辑|合成|管线|render|editing|montage/i,
  final_video: /渲染|剪辑|合成|管线|render|editing|montage/i,
  audio: /旁白|配音|音频|语音|audio|voice/i,
  subtitles: /字幕|subtitles/i,
  timeline: /剪辑|编排|管线|editing|montage/i,
  source_manifest: /素材|asset/i,
  project_manifest: /工程|交付|project|delivery/i,
  project_archive: /归档|交付|archive|delivery/i,
  quality_report: /审核|验收|质检|review|quality/i,
  delivery_manifest: /交付|归档|delivery/i,
}

const basename = (path: string) => path.replace(/\\/g, '/').split('/').pop()?.toLowerCase() ?? ''
const normalize = (value: string) => value.toLowerCase().replace(/[\s_-]/g, '')
function uniqueStage(stages: TaskLifecycleStage[]) {
  return stages.length === 1 ? stages[0] : undefined
}

function explicitlyProducesFile(stage: TaskLifecycleStage, fileName: string) {
  const normalizedName = fileName.toLowerCase()
  const hasProducerContext = (text: string) => {
    const normalizedText = text.toLowerCase()
    const fileIndex = normalizedText.lastIndexOf(normalizedName)
    if (fileIndex < 0) return false
    const before = normalizedText.slice(Math.max(0, fileIndex - 32), fileIndex)
    const after = normalizedText.slice(fileIndex + normalizedName.length, fileIndex + normalizedName.length + 12)
    return /(?:写入|写|生成|产出|输出|保存|创建|导出|制作)(?:文件|至|到|为)?\s*[:：`]*$/.test(before)
      || /^\s*(?:已|已经)?(?:写入|写|生成|产出|输出|保存|创建|导出|制作)/.test(after)
  }
  return hasProducerContext(stage.goal ?? '') || hasProducerContext(stage.latest_update ?? '')
}

/** Presentation grouping only: never changes file state or download eligibility.
 * Upload timestamps are intentionally not used: artifacts may be uploaded in a
 * batch at completion. Ambiguous matches stay visible without invented provenance.
 */
export function groupTaskStageFiles(
  files: TaskFile[],
  lifecycle?: TaskLifecycle,
  workflow?: WorkflowStatus | string | null,
) {
  const byStage = new Map<string, TaskFile[]>()
  const unassigned: TaskFile[] = []
  const historical: TaskFile[] = []
  const workStages = (lifecycle?.stages ?? []).filter((stage) => stage.kind === 'work' && stage.source === 'agent')
  const stages = workStages.filter((stage) => stage.state !== 'pending' && stage.state !== 'skipped')
  const parsed = parseWorkflowStatus(workflow)
  const workflowStages = Array.isArray(parsed?.stages) ? parsed.stages : []

  for (const file of files) {
    if (file.execution_id && lifecycle?.execution_id && file.execution_id !== lifecycle.execution_id) {
      historical.push(file)
      continue
    }
    const name = basename(file.file_name)
    const producerStage = file.producer_stage_id
      ? stages.find((stage) => stage.id === file.producer_stage_id)
      : undefined
    if (producerStage) {
      byStage.set(producerStage.id, [...(byStage.get(producerStage.id) ?? []), file])
      continue
    }
    const declarations = workflowStages.filter((stage) => stage.artifact_paths?.some((path) => basename(path) === name))
    const declaredMatches = stages.filter((stage) => declarations.some((declaration) =>
      normalize(declaration.key) === normalize(stage.id) || normalize(declaration.label) === normalize(stage.title),
    ))
    if (declaredMatches.length) {
      const stage = uniqueStage(declaredMatches)
      if (stage) byStage.set(stage.id, [...(byStage.get(stage.id) ?? []), file])
      else unassigned.push(file)
      continue
    }
    const explicitProducerMatches = stages.filter((stage) => explicitlyProducesFile(stage, name))
    const explicitProducer = uniqueStage(explicitProducerMatches)
    if (explicitProducer) {
      byStage.set(explicitProducer.id, [...(byStage.get(explicitProducer.id) ?? []), file])
      continue
    }
    if (explicitProducerMatches.length > 1) {
      unassigned.push(file)
      continue
    }
    const namedMatches = stages.filter((stage) => name && `${stage.goal ?? ''} ${stage.latest_update ?? ''}`.toLowerCase().includes(name))
    const pattern = roleStages[file.delivery_role ?? ''] ?? roleStages[file.role]
      ?? (name === 'montage-project.json' ? /剪辑|管线|montage|editing/i : undefined)
    const roleMatches = pattern ? stages.filter((stage) => pattern.test(`${stage.id} ${stage.title}`)) : []
    // Keep broad role inference ahead of incidental filename mentions. Only
    // explicit producer evidence above overrides it for legacy lifecycle data.
    const candidates = roleMatches.length ? roleMatches : namedMatches
    const stage = candidates.length ? uniqueStage(candidates) : workStages.length === 1 ? uniqueStage(stages) : undefined
    if (stage) byStage.set(stage.id, [...(byStage.get(stage.id) ?? []), file])
    else unassigned.push(file)
  }
  return { byStage, unassigned, historical }
}
