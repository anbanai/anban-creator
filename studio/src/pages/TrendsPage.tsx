import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "react-router-dom"
import { ExternalLink, Flame, RefreshCw, BookmarkPlus, WandSparkles } from "lucide-react"
import { toast } from "sonner"
import PageHeader from "@/components/layout/PageHeader"
import { ProjectContextControl } from "@/components/agent-prompt/ProjectContextControl"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api } from "@/lib/api"
import { queryKeys } from "@/lib/query-keys"
import type { TrendPlatformResult } from "@/types"

const platforms = ["weibo", "douyin", "zhihu", "bilibili", "baidu", "toutiao"]
const defaultPlatforms = ["weibo", "douyin", "zhihu"]
const labels: Record<string, string> = { weibo: "微博", douyin: "抖音", zhihu: "知乎", bilibili: "B站", baidu: "百度", toutiao: "头条" }

function relativeTime(value?: string) {
  if (!value) return "更新时间未知"
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return "更新时间未知"
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  return seconds < 60 ? "刚刚更新" : Math.floor(seconds / 60) + " 分钟前更新"
}

export function formatHot(value?: string) {
  if (!value) return "暂无热度"
  const normalized = value.trim()
  const numeric = Number(normalized.replace(/,/g, ""))
  if (!Number.isFinite(numeric)) return normalized
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 }).format(numeric)
}

type SaveInput = { projectId: string; title: string }

export default function TrendsPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [selectedPlatforms, setSelectedPlatforms] = useState(defaultPlatforms)
  const [limit, setLimit] = useState("6")
  const [projectId, setProjectId] = useState("")
  const [saveTarget, setSaveTarget] = useState<string | null>(null)
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ status: "active" }), queryFn: () => api.projects.list({ status: "active" }), staleTime: 60000 })
  const activePlatforms = useMemo(() => platforms.filter((platform) => selectedPlatforms.includes(platform)), [selectedPlatforms])
  const selectedPlatformsParam = activePlatforms.join(",")
  const trendsQuery = useQuery({
    queryKey: queryKeys.trends.all(selectedPlatformsParam, Number(limit)),
    queryFn: () => api.trends.list({ platforms: selectedPlatformsParam, limit: Number(limit) }),
    enabled: activePlatforms.length > 0,
    staleTime: 30000,
  })
  const refresh = useMutation({
    mutationFn: (nextPlatforms: string[]) => api.trends.refresh({ platforms: nextPlatforms, limit: Number(limit) }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.trends.all(selectedPlatformsParam, Number(limit)) })
      toast.success("热点已刷新")
    },
    onError: () => toast.error("热点刷新失败，请稍后重试"),
  })
  const save = useMutation({
    mutationFn: ({ projectId: targetProjectId, title }: SaveInput) => api.topicPool.create(targetProjectId, { topics: [title] }),
    onSuccess: async (data, variables) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.topicPool.all(variables.projectId) })
      toast.success(data.count ? "已收藏到项目选题池" : "该标题已在选题池中")
      setSaveTarget(null)
    },
    onError: () => toast.error("收藏失败"),
  })
  const projects = projectsQuery.data || []
  const groups = useMemo(() => trendsQuery.data?.items || [], [trendsQuery.data?.items])

  function useContent(item: TrendPlatformResult["items"][number]) {
    navigate("/", { state: { project_id: projectId || undefined, trend_title: item.title, trend_url: item.url } })
  }

  return <div className="mx-auto w-full max-w-6xl space-y-6 pb-10">
    <PageHeader title="热点雷达" description="聚合微博、抖音、知乎等平台的创作热点。">
      <Button variant="outline" onClick={() => refresh.mutate(activePlatforms)} disabled={refresh.isPending || activePlatforms.length === 0}>
        <RefreshCw className={refresh.isPending ? "animate-spin" : ""} />刷新热点
      </Button>
    </PageHeader>

    <section className="space-y-3" aria-label="热点筛选">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium">对比平台</p>
        </div>
        <Select value={limit} onValueChange={(value) => value && setLimit(value)}>
          <SelectTrigger className="w-32" aria-label="每个平台显示条数"><SelectValue /></SelectTrigger>
          <SelectContent>{[6, 12].map((n) => <SelectItem key={n} value={String(n)}>每个平台 {n} 条</SelectItem>)}</SelectContent>
        </Select>
      </div>
      <ToggleGroup
        multiple
        value={selectedPlatforms}
        onValueChange={(value) => setSelectedPlatforms(value)}
        variant="outline"
        size="sm"
        spacing={0}
        className="max-w-full flex-wrap"
        aria-label="对比平台"
      >
        {platforms.map((platform) => <ToggleGroupItem key={platform} value={platform}>{labels[platform]}</ToggleGroupItem>)}
      </ToggleGroup>
    </section>

    {trendsQuery.isError && <p role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">热点暂时不可用，请稍后重试。</p>}
    {activePlatforms.length === 0 && <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">至少选择一个平台查看热点。</p>}

    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {groups.map((group) => <Card key={group.platform}>
        <CardHeader className="flex-row items-center justify-between border-b">
          <CardTitle className="flex items-center gap-2"><Flame className="size-4 text-orange-500" />{group.label}</CardTitle>
          <div className="flex items-center gap-2">
            {group.stale && <Badge variant="outline">暂时沿用上次结果</Badge>}
            <Button size="icon-xs" variant="ghost" aria-label={"刷新" + group.label} title={"刷新" + group.label} onClick={() => refresh.mutate([group.platform])} disabled={refresh.isPending}><RefreshCw /></Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-1 pt-3">
          {group.items.map((item) => {
            const itemKey = `${group.platform}:${item.rank}:${item.title}`
            return <div key={itemKey} className="group flex items-start gap-2 rounded-lg p-2 hover:bg-muted">
              <span className={"w-5 shrink-0 text-center text-sm font-semibold " + (item.rank <= 3 ? "text-orange-500" : "text-muted-foreground")}>{item.rank}</span>
              <div className="min-w-0 flex-1">
                <div className="flex items-start gap-1"><a href={item.url || undefined} target="_blank" rel="noreferrer" className="line-clamp-2 text-sm hover:text-primary">{item.title}</a>{item.url && <ExternalLink className="mt-0.5 size-3 shrink-0 text-muted-foreground" />}</div>
                <div className="mt-1 text-xs text-muted-foreground">热度 {formatHot(item.hot)}</div>
              </div>
              <div className="flex shrink-0 gap-1 sm:opacity-0 sm:transition-opacity sm:group-hover:opacity-100 sm:group-focus-within:opacity-100">
                <Popover open={saveTarget === itemKey} onOpenChange={(open) => setSaveTarget(open ? itemKey : null)}>
                  <PopoverTrigger render={<Button size="icon-xs" variant="ghost" aria-label="收藏" title="收藏热点"><BookmarkPlus /></Button>} />
                  <PopoverContent align="end" className="w-80">
                    <div className="space-y-1"><PopoverTitle>收藏到项目</PopoverTitle><PopoverDescription>选择一个项目，之后可在选题池继续创作。</PopoverDescription></div>
                    <ProjectContextControl mode="select" projects={projects} value={projectId || null} onValueChange={(id) => setProjectId(id || "")} loading={projectsQuery.isLoading} placeholder="选择目标项目" ariaLabel="收藏目标项目" />
                    <Button size="sm" className="w-full" disabled={!projectId || save.isPending} onClick={() => projectId && save.mutate({ projectId, title: item.title })}>确认收藏</Button>
                  </PopoverContent>
                </Popover>
                <Button size="icon-xs" variant="ghost" aria-label="做内容" title="围绕热点做内容" onClick={() => useContent(item)}><WandSparkles /></Button>
              </div>
            </div>
          })}
          {group.items.length === 0 && <p className="py-6 text-center text-sm text-muted-foreground">暂无数据</p>}
          <div className="border-t pt-2 text-xs text-muted-foreground">{relativeTime(group.fetched_at)}{group.stale ? " · 暂时沿用上次结果" : ""}</div>
        </CardContent>
      </Card>)}
    </div>

    {!trendsQuery.isPending && activePlatforms.length > 0 && groups.length === 0 && <p className="py-12 text-center text-sm text-muted-foreground">暂无热点数据</p>}
    <p className="text-xs text-muted-foreground">数据会根据各平台更新情况自动刷新 · <Link className="text-primary" to="/">返回 AI 助手</Link></p>
  </div>
}
