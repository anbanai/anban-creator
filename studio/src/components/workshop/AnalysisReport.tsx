import type { AnalysisDimensionName, AnalysisResult, EvidenceDrivenDimension } from '@/types'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card'
import { Badge } from '@/components/ui/badge'

interface AnalysisReportProps {
  analysis: AnalysisResult
}

const dimensionLabels: Record<AnalysisDimensionName, string> = {
  topic_angle: '选题角度',
  title: '标题',
  cover: '封面',
  body: '正文',
  interaction: '互动',
  tags: '标签',
  comment_signals: '评论信号',
}

const orderedDimensions: AnalysisDimensionName[] = [
  'topic_angle',
  'title',
  'cover',
  'body',
  'interaction',
  'tags',
  'comment_signals',
]

const transferabilityLabel: Record<string, string> = {
  high: '高',
  medium: '中',
  low: '低',
}

const confidenceLabel: Record<string, string> = {
  high: '高',
  medium: '中',
  low: '低',
}

function scoreVariant(score: number): 'secondary' | 'outline' | 'destructive' {
  if (score > 80) return 'secondary'
  if (score > 60) return 'outline'
  return 'destructive'
}

function confidenceVariant(confidence: string): 'secondary' | 'outline' | 'destructive' {
  if (confidence === 'high') return 'secondary'
  if (confidence === 'medium') return 'outline'
  return 'destructive'
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function isValidAnalysisResult(value: AnalysisResult): boolean {
  if (!value || typeof value !== 'object') return false
  const candidate = value as unknown as Record<string, unknown>
  if (!isStringArray(candidate.summary) || candidate.summary.length < 3 || candidate.summary.length > 5) return false
  if (!Array.isArray(candidate.evidence_table) || candidate.evidence_table.length === 0) return false
  if (!Array.isArray(candidate.dimensions) || candidate.dimensions.length !== orderedDimensions.length) return false
  if (!candidate.clone_suggestions || typeof candidate.clone_suggestions !== 'object') return false
  if (!isStringArray(candidate.risks)) return false
  if (!['style-only', 'medium', 'tight'].includes(candidate.recommended_clone_depth as string)) return false

  const score = candidate.overall_score
  if (!score || typeof score !== 'object') return false
  const scoreMap = score as Record<string, unknown>
  if (typeof scoreMap.score !== 'number' || scoreMap.score < 0 || scoreMap.score > 100) return false
  if (!['high', 'medium', 'low'].includes(scoreMap.confidence as string)) return false
  if (typeof scoreMap.evidence_count !== 'number' || !isStringArray(scoreMap.missing_data) || !isNonEmptyString(scoreMap.why_not_higher)) return false

  const template = candidate.viral_template
  if (!template || typeof template !== 'object') return false
  const templateMap = template as Record<string, unknown>
  const requiredTemplateStrings = [
    templateMap.title_template,
    templateMap.cover_template,
    templateMap.body_template,
    templateMap.interaction_template,
    templateMap.tag_template,
    templateMap.audience_insight,
    templateMap.viral_mechanism,
  ]
  if (!requiredTemplateStrings.every(isNonEmptyString)) return false
  if (!isStringArray(templateMap.rewrite_constraints) || !isStringArray(templateMap.do_not_copy)) return false
  if (!['style-only', 'medium', 'tight'].includes(templateMap.recommended_clone_depth as string)) return false
  if (!['high', 'medium', 'low'].includes(templateMap.confidence as string)) return false

  const meta = candidate.template_meta
  if (!meta || typeof meta !== 'object') return false
  const metaMap = meta as Record<string, unknown>
  if (!isNonEmptyString(metaMap.name) || !isNonEmptyString(metaMap.source_feed_id) || !isNonEmptyString(metaMap.source_url)) return false
  if (metaMap.type !== 'seednote' || metaMap.category !== 'viral_analysis' || !isStringArray(metaMap.tags) || typeof metaMap.save_eligible !== 'boolean') return false

  const suggestionGroups = candidate.clone_suggestions as Record<string, unknown>
  if (!['title', 'body', 'cover', 'tags', 'interaction'].every((key) => isStringArray(suggestionGroups[key]))) return false

  const names = new Set<string>()
  for (const dimension of candidate.dimensions) {
    if (!dimension || typeof dimension !== 'object') return false
    const dimensionMap = dimension as Record<string, unknown>
    if (!orderedDimensions.includes(dimensionMap.name as AnalysisDimensionName)) return false
    if (names.has(dimensionMap.name as string)) return false
    names.add(dimensionMap.name as string)
    if (!isNonEmptyString(dimensionMap.observation) || !isNonEmptyString(dimensionMap.mechanism) || !isNonEmptyString(dimensionMap.action)) return false
    if (!['high', 'medium', 'low'].includes(dimensionMap.transferability as string)) return false
  }
  return orderedDimensions.every((name) => names.has(name))
}

function ReportField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs font-medium text-foreground">{label}</p>
      <p className="mt-1 leading-relaxed text-muted-foreground">{value}</p>
    </div>
  )
}

function DimensionCard({ dimension }: { dimension: EvidenceDrivenDimension }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <CardTitle>{dimensionLabels[dimension.name]}</CardTitle>
          <Badge variant={confidenceVariant(dimension.transferability)}>
            可迁移性{transferabilityLabel[dimension.transferability] ?? dimension.transferability}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <ReportField label="Observation" value={dimension.observation} />
        <ReportField label="Mechanism" value={dimension.mechanism} />
        <ReportField label="Transferability" value={transferabilityLabel[dimension.transferability] ?? dimension.transferability} />
        <ReportField label="Action" value={dimension.action} />
      </CardContent>
    </Card>
  )
}

function ActionList({ title, items }: { title: string; items: string[] }) {
  return (
    <div>
      <p className="text-sm font-medium text-foreground">{title}</p>
      <ul className="mt-2 space-y-1.5">
        {items.map((item, index) => (
          <li key={index} className="text-sm leading-relaxed text-muted-foreground">
            {item}
          </li>
        ))}
      </ul>
    </div>
  )
}

export default function AnalysisReport({ analysis }: AnalysisReportProps) {
  if (!isValidAnalysisResult(analysis)) {
    return (
      <Card>
        <CardContent className="py-8 text-center">
          <p className="text-sm font-medium text-foreground">报告结构无效</p>
          <p className="mt-1 text-sm text-muted-foreground">旧版本报告不再支持，请重新拆解。</p>
        </CardContent>
      </Card>
    )
  }

  const dimensionMap = new Map(analysis.dimensions.map((dimension) => [dimension.name, dimension]))
  const score = analysis.overall_score.score
  const missingData = analysis.overall_score.missing_data ?? []

  return (
    <div className="space-y-6">
      <Card>
        <CardContent className="flex flex-col gap-4 py-6 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <p className="text-sm text-muted-foreground">综合爆文指数</p>
            <p className="mt-1 text-4xl font-bold tabular-nums text-foreground">
              {score}
              <span className="ml-1 text-base font-normal text-muted-foreground">/100</span>
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Badge variant={scoreVariant(score)}>
              {score > 80 ? '优秀' : score > 60 ? '良好' : '待提升'}
            </Badge>
            <Badge variant={confidenceVariant(analysis.overall_score.confidence)}>
              置信度{confidenceLabel[analysis.overall_score.confidence] ?? analysis.overall_score.confidence}
            </Badge>
            <Badge variant="outline">证据 {analysis.overall_score.evidence_count}</Badge>
            <Badge variant="outline">推荐复刻：{analysis.recommended_clone_depth}</Badge>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>爆款结论摘要</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {analysis.summary.map((item, index) => (
            <div key={index} className="rounded-md border border-border p-3 text-sm leading-relaxed text-foreground">
              {item}
            </div>
          ))}
          <div className="space-y-2 rounded-md border border-border bg-muted/40 p-3 text-sm">
            <ReportField label="为什么没给更高分" value={analysis.overall_score.why_not_higher} />
            {missingData.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {missingData.map((item, index) => (
                  <Badge key={index} variant="outline" className="text-[11px]">
                    缺失：{item}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>证据表</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="py-2 pr-3 font-medium">结论</th>
                  <th className="py-2 pr-3 font-medium">证据</th>
                  <th className="py-2 font-medium">来源</th>
                </tr>
              </thead>
              <tbody>
                {analysis.evidence_table.map((row, index) => (
                  <tr key={index} className="border-b border-border/60 last:border-0">
                    <td className="py-2 pr-3 text-foreground">{row.claim}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{row.evidence}</td>
                    <td className="py-2 text-muted-foreground">{row.source}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {orderedDimensions.map((name) => {
          const dimension = dimensionMap.get(name)
          return dimension ? <DimensionCard key={name} dimension={dimension} /> : null
        })}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>复刻建议</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <ActionList title="标题" items={analysis.clone_suggestions.title} />
          <ActionList title="正文" items={analysis.clone_suggestions.body} />
          <ActionList title="封面" items={analysis.clone_suggestions.cover} />
          <ActionList title="标签" items={analysis.clone_suggestions.tags} />
          <ActionList title="互动" items={analysis.clone_suggestions.interaction} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>模板预览</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div className="grid gap-3 sm:grid-cols-2">
            <ReportField label="Title Template" value={analysis.viral_template.title_template} />
            <ReportField label="Cover Template" value={analysis.viral_template.cover_template} />
            <ReportField label="Body Template" value={analysis.viral_template.body_template} />
            <ReportField label="Interaction Template" value={analysis.viral_template.interaction_template} />
            <ReportField label="Tag Template" value={analysis.viral_template.tag_template} />
            <ReportField label="Audience Insight" value={analysis.viral_template.audience_insight} />
            <ReportField label="Viral Mechanism" value={analysis.viral_template.viral_mechanism} />
          </div>
          <ActionList title="Rewrite Constraints" items={analysis.viral_template.rewrite_constraints} />
          <ActionList title="Do Not Copy" items={analysis.viral_template.do_not_copy} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>风险提示</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline">推荐复刻深度：{analysis.recommended_clone_depth}</Badge>
            <Badge variant="outline">模板：{analysis.template_meta.name}</Badge>
            <Badge variant={analysis.template_meta.save_eligible ? 'secondary' : 'outline'}>
              {analysis.template_meta.save_eligible ? '可保存模板' : '不保存模板'}
            </Badge>
          </div>
          <ul className="space-y-2">
            {analysis.risks.map((risk, index) => (
              <li key={index} className="text-sm leading-relaxed text-muted-foreground">
                {risk}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}
