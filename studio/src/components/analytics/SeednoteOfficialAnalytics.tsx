import { BarChart3, PieChart as PieChartIcon } from 'lucide-react'
import { Bar, BarChart, Cell, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { seednoteAnalyticsViews, seednoteMetricsForView, type AnalyticsMetric, type AnalyticsOverview, type SeednoteAnalyticsViewKey } from '@/lib/content-analytics'

const sourceColors = ['#e11d48', '#f97316', '#eab308', '#14b8a6', '#6366f1', '#8b5cf6']

type Props = {
  data?: AnalyticsOverview
  error?: Error | null
  loading?: boolean
  view: SeednoteAnalyticsViewKey
  selectedMetric: string
  onViewChange: (view: SeednoteAnalyticsViewKey) => void
  onMetricChange: (metric: string) => void
}

function metricValue(metric: AnalyticsMetric, data?: AnalyticsOverview, loading?: boolean, error?: Error | null) {
  if (!metric.available) return '待接入'
  if (loading || error) return '—'
  const value = data?.totals[metric.key]
  return value == null ? '—' : metric.format(value)
}

function DataEmpty({ children }: { children: string }) {
  return <div className="flex min-h-48 flex-col items-center justify-center gap-2 text-sm text-muted-foreground"><BarChart3 className="size-7 opacity-40" /><p>{children}</p></div>
}

function WatchSources({ data }: { data?: AnalyticsOverview }) {
  const sources = data?.watch_sources
  if (!sources) return <DataEmpty>观看来源数据暂未接入</DataEmpty>
  if (!sources.length) return <DataEmpty>暂无观看来源数据</DataEmpty>
  const total = sources.reduce((sum, item) => sum + item.value, 0)
  return <div className="grid gap-5 lg:grid-cols-[minmax(220px,0.8fr)_1fr] lg:items-center"><div className="h-56 min-w-0"><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={sources} dataKey="value" nameKey="name" innerRadius="55%" outerRadius="78%" paddingAngle={2} isAnimationActive={false}>{sources.map((item, index) => <Cell key={item.name} fill={sourceColors[index % sourceColors.length]} />)}</Pie><Tooltip formatter={(value) => [`${Number(value).toLocaleString('zh-CN')}（${total ? ((Number(value) / total) * 100).toFixed(1) : '0.0'}%）`, '观看数']} /></PieChart></ResponsiveContainer></div><div className="grid gap-2" aria-label="观看来源图例">{sources.map((item, index) => { const percentage = total ? (item.value / total) * 100 : 0; return <div key={item.name} className="flex items-center justify-between gap-3 text-sm"><span className="flex min-w-0 items-center gap-2"><span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: sourceColors[index % sourceColors.length] }} /><span className="truncate">{item.name}</span></span><span className="shrink-0 tabular-nums text-muted-foreground">{percentage.toFixed(1)}%</span></div> })}</div></div>
}

function WatchHours({ data }: { data?: AnalyticsOverview }) {
  const hours = data?.watch_hours
  if (!hours) return <DataEmpty>观看时段数据暂未接入</DataEmpty>
  if (!hours.length) return <DataEmpty>暂无观看时段数据</DataEmpty>
  return <div className="h-64 min-w-0"><ResponsiveContainer width="100%" height="100%"><BarChart data={hours} margin={{ top: 8, right: 8, left: -12, bottom: 0 }}><XAxis dataKey="hour" tickLine={false} axisLine={false} tick={{ fontSize: 11 }} /><YAxis tickLine={false} axisLine={false} width={48} tick={{ fontSize: 11 }} /><Tooltip formatter={(value) => [Number(value).toLocaleString('zh-CN'), '观看数']} /><Bar dataKey="value" name="观看数" fill="var(--primary)" radius={[3, 3, 0, 0]} isAnimationActive={false} /></BarChart></ResponsiveContainer></div>
}

export default function SeednoteOfficialAnalytics({ data, error, loading = false, view, selectedMetric, onViewChange, onMetricChange }: Props) {
  const activeView = seednoteAnalyticsViews.find(item => item.key === view) ?? seednoteAnalyticsViews[0]
  const viewMetrics = seednoteMetricsForView(activeView.key)
  const trendMetrics = viewMetrics.filter(metric => metric.available !== false)
  const trendMetric = trendMetrics.find(metric => metric.key === selectedMetric)?.key ?? trendMetrics[0]?.key ?? viewMetrics[0]?.key ?? ''

  return <section aria-label="小红书官方统计" className="space-y-4">
    <Tabs value={activeView.key} onValueChange={value => { const nextView = seednoteAnalyticsViews.find(item => item.key === value)?.key; if (nextView) onViewChange(nextView) }}>
      <TabsList className="grid h-auto w-full grid-cols-2 sm:grid-cols-4">
        {seednoteAnalyticsViews.map(item => <TabsTrigger key={item.key} value={item.key} className="h-10">{item.label}</TabsTrigger>)}
      </TabsList>
      {seednoteAnalyticsViews.map(item => <TabsContent key={item.key} value={item.key} className="mt-4 space-y-4"><p className="text-sm text-muted-foreground">{item.description}</p>
        <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 lg:grid-cols-6" aria-label={`${item.label}指标`}>
          {seednoteMetricsForView(item.key).map(metric => { const unavailable = metric.available === false; return <button key={metric.key} type="button" disabled={unavailable} aria-disabled={unavailable} aria-pressed={!unavailable && trendMetric === metric.key} onClick={() => onMetricChange(metric.key)} className={`relative min-w-0 border-b border-r px-4 py-4 text-left transition-colors last:border-r-0 ${unavailable ? 'cursor-not-allowed opacity-60' : 'hover:bg-muted/50'} ${!unavailable && trendMetric === metric.key ? 'bg-primary/[0.045]' : ''}`}><span className="text-xs text-muted-foreground">{metric.label}</span><span className={`mt-2 block text-xl font-semibold tracking-tight tabular-nums ${unavailable ? 'text-sm font-medium' : ''}`}>{metricValue(metric, data, loading, error)}</span>{!unavailable && trendMetric === metric.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}</button> })}
        </div>
        <AnalyticsTrend title={`${item.label}趋势`} metrics={trendMetrics} data={error ? [] : data?.series ?? []} selectedMetric={trendMetric} onMetricChange={onMetricChange} loading={loading} description="累计口径；缺失记录不会补零。" emptyMessage={error ? '数据读取失败，请重试。' : '当前时间范围暂无可用趋势数据。'} />
        {item.key === 'watch' && <div className="grid gap-4 lg:grid-cols-2"><Card><CardHeader><CardTitle className="flex items-center gap-2"><PieChartIcon className="size-4" />观看来源</CardTitle></CardHeader><CardContent><WatchSources data={data} /></CardContent></Card><Card><CardHeader><CardTitle>观看时段</CardTitle></CardHeader><CardContent><WatchHours data={data} /></CardContent></Card></div>}
      </TabsContent>)}
    </Tabs>
  </section>
}
