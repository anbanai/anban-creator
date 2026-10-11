import { BarChart3, Clock3, Donut, Download, UsersRound } from 'lucide-react'
import { Bar, BarChart, Cell, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { AnalyticsMetric, SeednoteAnalyticsView, SeednoteAnalyticsViewKey } from '@/lib/content-analytics'
import type { AnalyticsOverview } from '@/lib/content-analytics'
import type { AnalyticsPoint } from '@/lib/analytics-period'

const sourceColors = ['#ff2442', '#ff7485', '#ffb0ba']

function formatMetric(metric: AnalyticsMetric, value: number | null | undefined) {
  return value == null ? '—' : metric.format(value)
}

function EmptyBreakdown({ icon: Icon, title, description }: { icon: typeof Donut; title: string; description: string }) {
  return <Card className="min-h-64">
    <CardHeader><CardTitle className="flex items-center gap-2 text-base"><Icon className="size-4 text-muted-foreground" />{title}</CardTitle><p className="text-xs text-muted-foreground">统计周期内的分布数据</p></CardHeader>
    <CardContent className="flex min-h-40 flex-col items-center justify-center gap-2 text-center text-sm text-muted-foreground"><BarChart3 className="size-7 opacity-40" /><p>{description}</p></CardContent>
  </Card>
}

export default function SeednoteOfficialAnalytics({
  view,
  views,
  overview,
  data,
  metrics,
  selectedMetric,
  onViewChange,
  onMetricChange,
  loading,
  emptyMessage,
}: {
  view: SeednoteAnalyticsViewKey
  views: readonly SeednoteAnalyticsView[]
  overview?: AnalyticsOverview
  data: AnalyticsPoint[]
  metrics: readonly AnalyticsMetric[]
  selectedMetric: string
  onViewChange: (view: SeednoteAnalyticsViewKey) => void
  onMetricChange: (key: string) => void
  loading?: boolean
  emptyMessage?: string
}) {
  const viewDefinition = views.find(item => item.key === view) ?? views[0]
  const sourceData = (overview as AnalyticsOverview & { watch_sources?: { name: string; value: number }[] } | undefined)?.watch_sources ?? []
  const timeData = (overview as AnalyticsOverview & { watch_hours?: { hour: string; value: number }[] } | undefined)?.watch_hours ?? []

  return <section aria-label="小红书官方统计" className="space-y-4">
    <Tabs value={view} onValueChange={value => onViewChange(value as SeednoteAnalyticsViewKey)}>
      <TabsList variant="line" className="w-full justify-start gap-5 overflow-x-auto rounded-none border-b p-0">
        {views.map(item => <TabsTrigger key={item.key} value={item.key} className="h-11 flex-none px-1 text-base">{item.label}</TabsTrigger>)}
      </TabsList>
    </Tabs>

    <div className="flex flex-wrap items-center justify-between gap-2">
      <div><h2 className="text-base font-semibold">数据总览</h2><p className="mt-1 text-xs text-muted-foreground">{viewDefinition.description}</p></div>
      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"><Download className="size-3.5" />导出数据可沿用当前导入记录</span>
    </div>

    <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:grid-cols-6" aria-label={`${viewDefinition.label}关键指标`}>
      {metrics.map((metric, index) => <button key={metric.key} type="button" aria-pressed={selectedMetric === metric.key} onClick={() => onMetricChange(metric.key)} className={`relative min-w-0 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 ${selectedMetric === metric.key ? 'bg-primary/[0.045]' : ''}`}>
        {selectedMetric === metric.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}
        <span className="text-xs text-muted-foreground">{metric.label}</span>
        <span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{loading ? '—' : formatMetric(metric, overview?.totals[metric.key])}</span>
        <span className="mt-1 block text-xs text-muted-foreground">{index === 0 ? '当前周期' : '区间数据'}</span>
      </button>)}
    </div>

    <AnalyticsTrend title={`${viewDefinition.label}趋势`} metrics={metrics} data={data} selectedMetric={selectedMetric} onMetricChange={onMetricChange} loading={loading} description="累计口径；缺失记录不补零。" emptyMessage={emptyMessage} />

    <div className="grid gap-4 lg:grid-cols-2">
      {sourceData.length > 0 ? <Card><CardHeader><CardTitle>观看来源</CardTitle></CardHeader><CardContent className="h-64"><ResponsiveContainer width="100%" height="100%"><PieChart><Pie data={sourceData} dataKey="value" nameKey="name" innerRadius="58%" outerRadius="78%" paddingAngle={2}>{sourceData.map((item, index) => <Cell key={item.name} fill={sourceColors[index % sourceColors.length]} />)}</Pie><Tooltip formatter={(value) => [`${value}%`, '占比']} /></PieChart></ResponsiveContainer></CardContent></Card> : <EmptyBreakdown icon={Donut} title="观看来源" description="暂未接入搜索、首页推荐等来源维度。" />}
      {timeData.length > 0 ? <Card><CardHeader><CardTitle>观看时段</CardTitle></CardHeader><CardContent className="h-64"><ResponsiveContainer width="100%" height="100%"><BarChart data={timeData}><XAxis dataKey="hour" tickLine={false} axisLine={false} /><YAxis allowDecimals={false} tickLine={false} axisLine={false} width={42} /><Tooltip /><Bar dataKey="value" fill="#ff2442" radius={[4, 4, 0, 0]} /></BarChart></ResponsiveContainer></CardContent></Card> : <EmptyBreakdown icon={Clock3} title="观看时段" description="暂未接入按小时聚合的观看数据。" />}
    </div>

    {(view === 'follower' || view === 'publication') && <div className="grid gap-4 sm:grid-cols-2">
      <Card size="sm"><CardContent className="flex items-start gap-3"><UsersRound className="mt-0.5 size-4 text-muted-foreground" /><div><p className="text-sm font-medium">分析提示</p><p className="mt-1 text-xs leading-relaxed text-muted-foreground">{view === 'follower' ? '涨粉视图需要主页访问和关注转化数据，当前接口未提供时会显示暂无数据。' : '发布视图可在后续接入发布频次与内容类型维度后，比较图文和视频的供给效果。'}</p></div></CardContent></Card>
    </div>}
  </section>
}
