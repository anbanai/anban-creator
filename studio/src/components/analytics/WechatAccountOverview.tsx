import { AlertCircle, CalendarDays, CheckCircle2, Database, Minus, TrendingDown, TrendingUp } from 'lucide-react'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { formatWechatComparison, wechatAccountMetrics, type WechatAccountMetric, type WechatAccountOverview as Overview, type WechatComparisonSet } from '@/lib/wechat-account-analytics'

function MetricValue({ value, format }: { value: number | null | undefined; format: (value: number) => string }) {
  return <span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{value == null ? '—' : format(value)}</span>
}

function Comparison({ value, label }: { value: number | null | undefined; label: string }) {
  const Icon = value == null ? Minus : value >= 0 ? TrendingUp : TrendingDown
  return <span className="inline-flex items-center gap-1 text-xs text-muted-foreground"><Icon className="size-3" />{label} {formatWechatComparison(value)}</span>
}

function Comparisons({ comparison }: { comparison?: WechatComparisonSet | null }) {
  if (!comparison) return null
  const entries = [comparison.previous_day, comparison.previous_period, comparison.previous_week].filter((item): item is NonNullable<typeof item> => Boolean(item))
  if (!entries.length) return <Comparison value={null} label="比较" />
  return <span className="flex flex-col items-start gap-0.5">{entries.map((item) => <Comparison key={`${item.label}:${item.from ?? ''}:${item.to ?? ''}`} value={item?.value} label={item?.label ?? '比较'} />)}</span>
}

export default function WechatAccountOverviewPanel({
  overview,
  loading,
  error,
  selectedMetric = 'read_users',
  onMetricChange,
}: {
  overview?: Overview
  loading?: boolean
  error?: Error
  selectedMetric?: WechatAccountMetric
  onMetricChange?: (metric: WechatAccountMetric) => void
}) {
  const metric = wechatAccountMetrics.find(item => item.key === selectedMetric) ?? wechatAccountMetrics[0]
  const coverage = overview?.coverage
  const series = overview?.series ?? []
  const unavailable = error?.message || overview?.unavailable_metrics?.[selectedMetric]
  return <section aria-label="公众号数据概况" className="space-y-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h2 className="text-base font-semibold">账号数据概况</h2><p className="mt-1 text-xs text-muted-foreground">公众号账号级每日统计，与单篇内容累计数据分开计算。</p></div>
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5"><CalendarDays className="size-3.5" />{overview ? `${overview.from} 至 ${overview.to}` : '等待数据'}</span>
        {overview?.data_through && <span>数据截至 {overview.data_through}</span>}
      </div>
    </div>
    {error && <Alert variant="destructive"><AlertCircle /><AlertDescription>账号统计暂时无法读取：{error.message}</AlertDescription></Alert>}
    {!error && coverage && <div className={`flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-xs ${coverage.complete ? 'border-emerald-500/30 bg-emerald-500/[0.04] text-emerald-700 dark:text-emerald-300' : 'border-amber-500/30 bg-amber-500/[0.05] text-amber-700 dark:text-amber-300'}`} role="status">
      {coverage.complete ? <CheckCircle2 className="size-3.5" /> : <Database className="size-3.5" />}<span>数据覆盖 {coverage.covered_days} / {coverage.expected_days} 天</span>{!coverage.complete && <span>，比较值暂不可用。</span>}
    </div>}
    <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:grid-cols-6" aria-label="公众号账号关键指标">
      {wechatAccountMetrics.slice(0, 6).map(item => <button key={item.key} type="button" aria-pressed={selectedMetric === item.key} onClick={() => onMetricChange?.(item.key)} className={`relative min-w-0 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 ${selectedMetric === item.key ? 'bg-primary/[0.045]' : ''}`}>
        {selectedMetric === item.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}<span className="text-xs text-muted-foreground">{item.label}</span><MetricValue value={loading ? null : overview?.totals[item.key]} format={item.format} />
        <Comparisons comparison={overview?.comparisons?.[item.key]} />
      </button>)}
    </div>
    {unavailable && !error && <p className="text-xs text-muted-foreground">{metric.label}：{unavailable}</p>}
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-card px-3 py-2">
      <span className="text-xs font-medium">趋势筛选</span>
      <Select value={selectedMetric} onValueChange={value => value && onMetricChange?.(value as WechatAccountMetric)}><SelectTrigger size="sm" aria-label="账号趋势指标" className="min-w-32"><SelectValue>{metric.label}</SelectValue></SelectTrigger><SelectContent>{wechatAccountMetrics.map(item => <SelectItem key={item.key} value={item.key}>{item.label}</SelectItem>)}</SelectContent></Select>
    </div>
    <AnalyticsTrend title="账号流量趋势" metrics={wechatAccountMetrics} data={series} selectedMetric={metric.key} onMetricChange={value => onMetricChange?.(value as WechatAccountMetric)} loading={loading} description="按日账号数据；缺失日期保留为空，不补零。" emptyMessage={error ? '账号统计读取失败，请重试。' : '当前时间范围暂无账号数据，请导入官方统计。'} />
  </section>
}
