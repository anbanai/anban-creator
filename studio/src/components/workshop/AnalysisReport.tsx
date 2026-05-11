import {
  Radar,
  RadarChart,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  ResponsiveContainer,
  Tooltip,
} from 'recharts'
import type { AnalysisResult } from '@/types'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card'
import { Badge } from '@/components/ui/badge'

interface AnalysisReportProps {
  analysis: AnalysisResult
}

function scoreVariant(score: number): 'secondary' | 'outline' | 'destructive' {
  if (score > 80) return 'secondary'
  if (score > 60) return 'outline'
  return 'destructive'
}

const dimensionCards: Array<{
  key: keyof AnalysisResult
  label: string
  extractDetail: (analysis: AnalysisResult) => { score: number; breakdown: string; elements: string[] }
}> = [
  {
    key: 'title_analysis',
    label: '标题分析',
    extractDetail: (a) => ({
      score: a.title_analysis.score,
      breakdown: a.title_analysis.breakdown,
      elements: [a.title_analysis.technique, ...a.title_analysis.rewrite_suggestions],
    }),
  },
  {
    key: 'cover_analysis',
    label: '封面分析',
    extractDetail: (a) => ({
      score: a.cover_analysis.score,
      breakdown: a.cover_analysis.breakdown,
      elements: [a.cover_analysis.style, ...a.cover_analysis.key_elements],
    }),
  },
  {
    key: 'copywriting_analysis',
    label: '文案分析',
    extractDetail: (a) => ({
      score: a.copywriting_analysis.score,
      breakdown: a.copywriting_analysis.breakdown,
      elements: [
        `结构：${a.copywriting_analysis.structure}`,
        `字数：${a.copywriting_analysis.word_count}`,
        ...a.copywriting_analysis.formulas_used,
      ],
    }),
  },
  {
    key: 'tag_analysis',
    label: '标签分析',
    extractDetail: (a) => ({
      score: a.tag_analysis.score,
      breakdown: a.tag_analysis.breakdown,
      elements: [...a.tag_analysis.suggested_tags],
    }),
  },
  {
    key: 'interaction_analysis',
    label: '互动分析',
    extractDetail: (a) => ({
      score: a.interaction_analysis.score,
      breakdown: a.interaction_analysis.breakdown,
      elements: [
        `CTA：${a.interaction_analysis.cta_type}`,
        ...a.interaction_analysis.techniques,
      ],
    }),
  },
]

export default function AnalysisReport({ analysis }: AnalysisReportProps) {
  const radarData = [
    { dimension: '选题', value: analysis.viral_factors.topic },
    { dimension: '标题', value: analysis.viral_factors.title },
    { dimension: '内容', value: analysis.viral_factors.content },
    { dimension: '视觉', value: analysis.viral_factors.visual },
    { dimension: '互动', value: analysis.viral_factors.interaction },
  ]

  return (
    <div className="space-y-6">
      {/* Overall Score */}
      <Card>
        <CardContent className="flex flex-col items-center gap-3 py-6 sm:flex-row sm:justify-center sm:gap-6">
          <div className="text-center">
            <p className="text-sm text-muted-foreground">综合爆文指数</p>
            <p className="mt-1 text-4xl font-bold tabular-nums text-foreground">
              {analysis.overall_score}
              <span className="ml-1 text-base font-normal text-muted-foreground">/100</span>
            </p>
          </div>
          <Badge variant={scoreVariant(analysis.overall_score)} className="text-sm px-3 py-1">
            {analysis.overall_score > 80 ? '优秀' : analysis.overall_score > 60 ? '良好' : '待提升'}
          </Badge>
        </CardContent>
      </Card>

      {/* Radar Chart */}
      <Card>
        <CardHeader>
          <CardTitle>爆文因子雷达图</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="mx-auto h-72 max-w-md">
            <ResponsiveContainer width="100%" height="100%">
              <RadarChart data={radarData} cx="50%" cy="50%" outerRadius="70%">
                <PolarGrid className="stroke-border" />
                <PolarAngleAxis
                  dataKey="dimension"
                  tick={{ fontSize: 12 }}
                  stroke="var(--muted-foreground)"
                />
                <PolarRadiusAxis
                  angle={90}
                  domain={[0, 100]}
                  tick={{ fontSize: 10 }}
                  stroke="var(--muted-foreground)"
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--background)',
                    color: 'var(--foreground)',
                    border: '1px solid var(--border)',
                    borderRadius: 'var(--radius)',
                    fontSize: '12px',
                  }}
                />
                <Radar
                  name="爆文指数"
                  dataKey="value"
                  stroke="var(--primary)"
                  fill="var(--primary)"
                  fillOpacity={0.2}
                  strokeWidth={2}
                />
              </RadarChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      {/* Dimension Cards */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {dimensionCards.map(({ key, label, extractDetail }) => {
          const detail = extractDetail(analysis)
          return (
            <Card key={key}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle>{label}</CardTitle>
                  <Badge variant={scoreVariant(detail.score)}>{detail.score}</Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <p className="text-sm text-muted-foreground leading-relaxed">{detail.breakdown}</p>
                {detail.elements.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {detail.elements.filter(Boolean).map((el, i) => (
                      <Badge key={i} variant="outline" className="text-[11px]">
                        {el}
                      </Badge>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>

      {/* Suggestions */}
      {analysis.suggestions.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>优化建议</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2">
              {analysis.suggestions.map((suggestion, i) => (
                <li key={i} className="flex items-start gap-2 text-sm text-muted-foreground">
                  <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-medium text-primary">
                    {i + 1}
                  </span>
                  <span className="leading-relaxed">{suggestion}</span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
