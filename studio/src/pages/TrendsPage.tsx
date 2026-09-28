import { useEffect, useMemo, useRef, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "react-router-dom"
import { ExternalLink, Flame, RefreshCw, BookmarkPlus, WandSparkles } from "lucide-react"
import { toast } from "sonner"
import PageHeader from "@/components/layout/PageHeader"
import { ProjectContextControl } from "@/components/agent-prompt/ProjectContextControl"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select"
import { api } from "@/lib/api"
import { queryKeys } from "@/lib/query-keys"
import type { TrendPlatformResult } from "@/types"

const platforms = ["weibo", "douyin", "zhihu", "bilibili", "baidu", "toutiao"]
const labels: Record<string, string> = { weibo: "微博", douyin: "抖音", zhihu: "知乎", bilibili: "B站", baidu: "百度", toutiao: "头条" }
function relativeTime(value?: string) { if (!value) return "尚未抓取"; const seconds = Math.max(0, Math.floor((Date.now() - Date.parse(value)) / 1000)); return seconds < 60 ? "刚刚更新" : Math.floor(seconds / 60) + " 分钟前更新" }

export default function TrendsPage() {
  const navigate = useNavigate(); const queryClient = useQueryClient()
  const [platform, setPlatform] = useState("all"); const [limit, setLimit] = useState("12"); const [projectId, setProjectId] = useState("")
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ status: "active" }), queryFn: () => api.projects.list({ status: "active" }), staleTime: 60000 })
  const autoSelectedProject = useRef(false)
  useEffect(() => {
    if (!autoSelectedProject.current && projectsQuery.data?.[0]?.id) {
      autoSelectedProject.current = true
      setProjectId(projectsQuery.data[0].id)
    }
  }, [projectsQuery.data])
  const selectedPlatforms = platform === "all" ? undefined : platform
  const trendsQuery = useQuery({ queryKey: queryKeys.trends.all(selectedPlatforms, Number(limit)), queryFn: () => api.trends.list({ platforms: selectedPlatforms, limit: Number(limit) }), staleTime: 30000 })
  const refresh = useMutation({ mutationFn: (p?: string) => api.trends.refresh({ platforms: p ? [p] : selectedPlatforms ? [selectedPlatforms] : undefined, limit: Number(limit) }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: queryKeys.trends.all(selectedPlatforms, Number(limit)) }); toast.success("热点已刷新") }, onError: () => toast.error("热点刷新失败，请稍后重试") })
  const save = useMutation({ mutationFn: (title: string) => api.topicPool.create(projectId, { topics: [title] }), onSuccess: async (data) => { await queryClient.invalidateQueries({ queryKey: queryKeys.topicPool.all(projectId) }); toast.success(data.count ? "已收藏到项目选题池" : "该标题已在选题池中") }, onError: () => toast.error("收藏失败") })
  const projects = projectsQuery.data || []; const groups = trendsQuery.data?.items || []
  const shown = useMemo(() => groups.filter((g) => platform === "all" || g.platform === platform), [groups, platform])
  function useContent(item: TrendPlatformResult["items"][number]) { navigate("/", { state: { project_id: projectId, trend_title: item.title, trend_url: item.url } }) }
  return <div className="mx-auto w-full max-w-6xl space-y-6 pb-10">
    <PageHeader title="热点雷达" description="聚合六个平台最新热点，按统一 TTL 自动保持数据新鲜。"><Button variant="outline" onClick={() => refresh.mutate()} disabled={refresh.isPending}><RefreshCw className={refresh.isPending ? "animate-spin" : ""} />全部刷新</Button></PageHeader>
    <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-card p-3"><ProjectContextControl mode="select" projects={projects} value={projectId || null} allowNoProject noProjectLabel="不选择项目" onValueChange={(id) => setProjectId(id || "")} loading={projectsQuery.isLoading} placeholder="选择项目以收藏热点" compact /><div className="flex flex-wrap gap-1">{["all", ...platforms].map((p) => <Button key={p} size="sm" variant={platform === p ? "default" : "outline"} onClick={() => setPlatform(p)}>{p === "all" ? "全部平台" : labels[p]}</Button>)}</div><Select value={limit} onValueChange={(value) => value && setLimit(value)}><SelectTrigger className="w-24"><SelectValue /></SelectTrigger><SelectContent>{[6, 12, 24, 50].map((n) => <SelectItem key={n} value={String(n)}>{n} 条</SelectItem>)}</SelectContent></Select></div>
    {trendsQuery.isError && <p role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">热点暂时不可用，请稍后重试。</p>}
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{shown.map((group) => <Card key={group.platform}><CardHeader className="flex-row items-center justify-between border-b"><CardTitle className="flex items-center gap-2"><Flame className="size-4 text-orange-500" />{group.label}</CardTitle><div className="flex items-center gap-2">{group.stale && <Badge variant="destructive">stale</Badge>}<Button size="icon-xs" variant="ghost" aria-label={"刷新" + group.label} title={"刷新" + group.label} onClick={() => refresh.mutate(group.platform)} disabled={refresh.isPending}><RefreshCw /></Button></div></CardHeader><CardContent className="space-y-1 pt-3">{group.items.map((item) => <div key={group.platform + item.rank + item.title} className="group flex items-start gap-2 rounded-lg p-2 hover:bg-muted"><span className={"w-5 shrink-0 text-center text-sm font-semibold " + (item.rank <= 3 ? "text-orange-500" : "text-muted-foreground")}>{item.rank}</span><div className="min-w-0 flex-1"><div className="flex items-start gap-1"><a href={item.url || undefined} target="_blank" rel="noreferrer" className="line-clamp-2 text-sm hover:text-primary">{item.title}</a>{item.url && <ExternalLink className="mt-0.5 size-3 shrink-0 text-muted-foreground" />}</div><div className="mt-1 text-xs text-muted-foreground">{item.hot || "—"}</div></div><div className="flex shrink-0 gap-1 sm:opacity-0 sm:transition-opacity sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"><Button size="icon-xs" variant="ghost" aria-label="收藏" title="收藏热点" disabled={save.isPending} onClick={() => projectId ? save.mutate(item.title) : toast.info("请先选择或创建项目")}><BookmarkPlus /></Button><Button size="icon-xs" variant="ghost" aria-label="做内容" title="围绕热点做内容" onClick={() => useContent(item)}><WandSparkles /></Button></div></div>)}{group.items.length === 0 && <p className="py-6 text-center text-sm text-muted-foreground">暂无数据</p>}<div className="border-t pt-2 text-xs text-muted-foreground">{relativeTime(group.fetched_at)} · {group.stale ? (group.last_error || "上游刷新失败，展示旧数据") : "数据有效期内"}</div></CardContent></Card>)}</div>
    {!trendsQuery.isPending && shown.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">暂无热点数据</p>}
    <p className="text-xs text-muted-foreground">本次读取于 {trendsQuery.data?.requested_at ? new Date(trendsQuery.data.requested_at).toLocaleTimeString() : "—"} 完成 · TTL {trendsQuery.data?.ttl_seconds || 0} 秒 · <Link className="text-primary" to="/">返回 AI 助手</Link></p>
  </div>
}
