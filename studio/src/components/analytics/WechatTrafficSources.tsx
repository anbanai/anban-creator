import { AlertCircle, BarChart3 } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import { wechatAccountAnalyticsApi, wechatAccountMetrics, wechatTrafficMetrics, wechatTrafficSources, type WechatAccountMetric, type WechatTrafficSource } from '@/lib/wechat-account-analytics'

export default function WechatTrafficSources({ projectId, from, to, metric = 'read_users', source = 'all', onMetricChange, onSourceChange }: { projectId: string; from: string; to: string; metric?: WechatAccountMetric; source?: WechatTrafficSource; onMetricChange?: (metric: WechatAccountMetric) => void; onSourceChange?: (source: WechatTrafficSource) => void }) {
  const query = useQuery({ queryKey: ['wechat-account-analytics', projectId, 'traffic-sources', from, to, metric, source], queryFn: ({ signal }) => wechatAccountAnalyticsApi.trafficSources(projectId, { from, to, metric, ...(source !== 'all' ? { source } : {}) }, signal), enabled: Boolean(projectId && from && to) })
  const metricLabel = wechatAccountMetrics.find(item => item.key === metric)?.label ?? metric
  return <Card aria-label="公众号流量来源">
    <CardHeader className="gap-3"><div className="flex flex-wrap items-center justify-between gap-3"><div><CardTitle>流量来源</CardTitle><p className="mt-1 text-xs text-muted-foreground">按来源查看{metricLabel}，占比由服务端按覆盖范围计算。</p></div><div className="flex flex-wrap gap-2"><Select value={metric} onValueChange={value => value && onMetricChange?.(value as WechatAccountMetric)}><SelectTrigger size="sm" aria-label="流量来源指标" className="min-w-28"><SelectValue>{metricLabel}</SelectValue></SelectTrigger><SelectContent>{wechatTrafficMetrics.map(item => <SelectItem key={item.key} value={item.key}>{item.label}</SelectItem>)}</SelectContent></Select><Select value={source} onValueChange={value => value && onSourceChange?.(value as WechatTrafficSource)}><SelectTrigger size="sm" aria-label="流量来源" className="min-w-28"><SelectValue>{wechatTrafficSources.find(item => item.value === source)?.label}</SelectValue></SelectTrigger><SelectContent>{wechatTrafficSources.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent></Select></div></div></CardHeader>
    <CardContent>
      {query.isPending && <p role="status" className="py-12 text-center text-sm text-muted-foreground">正在读取来源数据…</p>}
      {query.isError && <div role="alert" className="flex items-center justify-center gap-2 py-12 text-sm text-destructive"><AlertCircle className="size-4" />来源数据读取失败：{query.error.message}</div>}
      {!query.isPending && !query.isError && query.data && <>
        {query.data.data_quality && <p className="mb-3 rounded-md bg-amber-500/[0.08] px-3 py-2 text-xs text-amber-700 dark:text-amber-300">{query.data.data_quality}</p>}
        <AnalyticsTrend
          title="来源阅读趋势"
          metrics={[wechatAccountMetrics[0]]}
          data={query.data.series ?? []}
          selectedMetric="read_users"
          onMetricChange={() => undefined}
          description={source === 'all' ? '按日账号阅读人数；选择具体来源后显示该来源的阅读人数。' : '按日展示所选来源的阅读人数。来源趋势仅支持阅读人数。'}
          emptyMessage={query.data.items.length ? '当前指标暂无可用的来源趋势。' : '当前时间范围暂无来源数据。'}
        />
        {query.data.items.length === 0 ? <div className="flex flex-col items-center justify-center gap-2 py-12 text-sm text-muted-foreground"><BarChart3 className="size-7 opacity-40" /><p>当前时间范围暂无来源数据。</p></div> : <div className="overflow-x-auto" aria-label="流量来源明细"><Table><TableHeader><TableRow><TableHead>来源</TableHead><TableHead className="text-right">人数</TableHead><TableHead className="text-right">占比</TableHead></TableRow></TableHeader><TableBody>{query.data.items.map(item => <TableRow key={item.source}><TableCell className="font-medium">{item.label}</TableCell><TableCell className="text-right tabular-nums">{item.value == null ? '—' : item.value.toLocaleString('zh-CN')}</TableCell><TableCell className="text-right tabular-nums">{item.share == null ? '—' : `${(item.share * 100).toFixed(2)}%`}</TableCell></TableRow>)}</TableBody></Table></div>}
      </>}
    </CardContent>
  </Card>
}
