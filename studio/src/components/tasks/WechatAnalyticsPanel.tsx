import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertTriangle,
  Bookmark,
  CheckCircle2,
  ExternalLink,
  Eye,
  Loader2,
  MessageCircle,
  RefreshCw,
  Send,
  Share2,
  Star,
  Timer,
  UserRound,
  UserPlus,
} from "lucide-react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import type {
  ProjectConfig,
  TaskOutcome,
  WechatAnalytics,
  WechatPublication,
  WechatPublishMode,
} from "@/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const statusCopy: Record<string, string> = {
  drafting: "正在创建草稿",
  drafted: "已进入草稿箱",
  publish_submitting: "正在提交发布",
  publishing: "微信发布处理中",
  published: "已识别正式发布",
  needs_selection: "需要选择文章",
  publish_failed: "发布失败",
  unsupported: "公众号接口调用失败",
};

const terminalPublicationStatuses = new Set([
  "published",
  "publish_failed",
  "unsupported",
]);

const notifyFormalPublishResult = (publication: WechatPublication) => {
  if (publication.status !== "unsupported" || publication.wechat_status_code !== 48001) return;
  toast.error("正式发布失败：公众号没有接口权限", {
    description: "微信错误码 48001。草稿已保留，请前往公众号后台手动发布；权限开通后可在这里重新尝试。",
  });
};

export default function WechatAnalyticsPanel({
  taskId,
  projectConfig,
  taskOutcome,
}: {
  taskId: string;
  projectConfig?: ProjectConfig;
  taskOutcome?: TaskOutcome;
}) {
  const queryClient = useQueryClient();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const mode = projectConfig?.wechat_publish_mode ?? "manual";
  const publicationQuery = useQuery({
    queryKey: ["wechat-publication", taskId],
    queryFn: () => api.tasks.getWechatPublication(taskId),
    enabled: Boolean(taskId) && mode !== "disabled",
    retry: false,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && !terminalPublicationStatuses.has(status) ? 5_000 : false;
    },
  });
  const publication = publicationQuery.data;
  const reconcile = useMutation({
    mutationFn: () => api.tasks.reconcileWechat(taskId),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ["wechat-publication", taskId],
      }),
  });
  const publish = useMutation({
    mutationFn: () => api.tasks.publishWechat(taskId),
    onSuccess: (result) => {
      setConfirmOpen(false);
      notifyFormalPublishResult(result);
      void queryClient.invalidateQueries({
        queryKey: ["wechat-publication", taskId],
      });
    },
  });
  const retryPublish = useMutation({
    mutationFn: () => api.tasks.retryWechatPublish(taskId),
    onSuccess: (result) => {
      notifyFormalPublishResult(result);
      void queryClient.invalidateQueries({
        queryKey: ["wechat-publication", taskId],
      });
    },
  });
  const select = useMutation({
    mutationFn: (articleId: string) =>
      api.tasks.selectWechatArticle(taskId, articleId),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ["wechat-publication", taskId],
      }),
  });
  if (mode === "disabled") return null;
  if (publicationQuery.isLoading)
    return (
      <Card>
        <CardContent className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          公众号发布状态加载中
        </CardContent>
      </Card>
    );
  const publicationErrorStatus = (
    publicationQuery.error as { response?: { status?: number } } | null
  )?.response?.status;
  if (publicationQuery.isError && publicationErrorStatus === 404)
    return <MissingPublicationState mode={mode} outcome={taskOutcome} />;
  if (publicationQuery.isError)
    return (
      <Card>
        <CardContent className="text-sm text-destructive">
          公众号发布状态暂时无法加载，请稍后重试。
        </CardContent>
      </Card>
    );
  if (!publication)
    return <MissingPublicationState mode={mode} outcome={taskOutcome} />;
  const mutationFailed = reconcile.isError || publish.isError || retryPublish.isError || select.isError;
  return (
    <div className="space-y-4">
      <PublicationCard
        publication={publication}
        mode={mode}
        onReconcile={() => reconcile.mutate()}
        onPublish={() => setConfirmOpen(true)}
        onRetryPublish={() => retryPublish.mutate()}
        pending={reconcile.isPending || publish.isPending || retryPublish.isPending}
      />
      {mutationFailed && (
        <p className="flex items-center gap-2 text-sm text-destructive" role="alert">
          <AlertTriangle className="h-4 w-4" />
          公众号发布操作失败，请稍后重试。
        </p>
      )}
      {publication.status === "needs_selection" && (
        <CandidatePicker
          publication={publication}
          pending={select.isPending}
          onSelect={(id) => select.mutate(id)}
        />
      )}
      {publication.status === "published" && (
        <WechatAnalyticsContent taskId={taskId} />
      )}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认正式发布</DialogTitle>
            <DialogDescription>
              将使用当前公众号账号把已生成的草稿提交为正式文章，提交后会继续确认微信发布结果。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              取消
            </Button>
            <Button
              onClick={() => publish.mutate()}
              disabled={publish.isPending}
            >
              {publish.isPending && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              确认发布
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function MissingPublicationState({
  mode,
  outcome,
}: {
  mode: WechatPublishMode;
  outcome?: TaskOutcome;
}) {
  const status = outcome?.publication.status;
  if (!status || status === "not_requested") {
    return (
      <Card>
        <CardContent className="text-sm text-muted-foreground">
          {outcome ? "本次任务没有发起公众号投递。" : "该任务尚未创建公众号草稿。"}
        </CardContent>
      </Card>
    );
  }

  const ambiguous = status === "ambiguous";
  const succeeded = status === "succeeded";
  const automatic = mode === "api_confirmed";
  const title = succeeded
    ? "公众号草稿已创建。"
    : ambiguous
    ? "微信是否收到草稿请求暂时无法确认。"
    : automatic
      ? "本次未创建公众号草稿，自动发布没有启动。"
      : "本次未创建公众号草稿。";
  const detail = succeeded
    ? "草稿详情暂时无法加载，请稍后刷新。"
    : ambiguous
    ? "为避免重复投稿，系统不会再次提交；请先到公众号后台核对草稿箱。"
    : outcome.publication.message || (status === "skipped"
      ? "发布前检查阻止了公众号投递，请根据任务警告修改文章后重新执行。"
      : "请根据任务警告修复问题后重新执行。"
    );
  const StateIcon = succeeded ? CheckCircle2 : AlertTriangle;

  return (
    <Card className={succeeded ? "border-emerald-300 dark:border-emerald-800" : "border-amber-300 dark:border-amber-800"}>
      <CardHeader className="gap-3 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <CardTitle className="flex items-start gap-2 text-base">
            <StateIcon className={succeeded
              ? "mt-0.5 h-4 w-4 shrink-0 text-emerald-700 dark:text-emerald-400"
              : "mt-0.5 h-4 w-4 shrink-0 text-amber-700 dark:text-amber-400"
            } />
            <span>{title}</span>
          </CardTitle>
          <p className="mt-2 text-sm text-muted-foreground">{detail}</p>
        </div>
        {ambiguous && (
          <Button
            size="sm"
            variant="outline"
            nativeButton={false}
            render={<a href="https://mp.weixin.qq.com/" target="_blank" rel="noreferrer" />}
          >
            <ExternalLink className="h-4 w-4" />
            打开公众号后台
          </Button>
        )}
      </CardHeader>
    </Card>
  );
}

function PublicationCard({
  publication,
  mode,
  onReconcile,
  onPublish,
  onRetryPublish,
  pending,
}: {
  publication: WechatPublication;
  mode: WechatPublishMode;
  onReconcile: () => void;
  onPublish: () => void;
  onRetryPublish: () => void;
  pending: boolean;
}) {
  const status = publication.status;
  const hasSubmissionEvidence = Boolean(
    publication.submit_attempted_at ||
      publication.publish_id ||
      publication.msg_data_id ||
      publication.msg_id,
  );
  const canPublish = mode === "manual" && status === "drafted" && !hasSubmissionEvidence;
  const canRetryPublish =
    status === "unsupported" &&
    Boolean(publication.wechat_status_code) &&
    Boolean(publication.draft_media_id);
  const canReconcile =
    status !== "publishing" && !terminalPublicationStatuses.has(status);
  return (
    <Card
      className={
        status === "publish_failed" ? "border-destructive/40" : undefined
      }
    >
      <CardHeader className="gap-3 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>公众号发布</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge
              variant={
                status === "publish_failed" || status === "unsupported"
                  ? "destructive"
                  : status === "published"
                    ? "default"
                    : "outline"
              }
            >
              {statusCopy[status] ?? status}
            </Badge>
            <span className="text-xs text-muted-foreground">
              {publication.source === "wechat_console"
                ? "公众号后台发布"
                : "Anban 提交"}
            </span>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {(status === "drafted" || canRetryPublish) && (
            <Button
              size="sm"
              variant="outline"
              nativeButton={false}
              render={<a href="https://mp.weixin.qq.com/" target="_blank" rel="noreferrer" />}
            >
              <ExternalLink className="h-4 w-4" />
              打开公众号后台
            </Button>
          )}
          {publication.article_url && (
            <Button
              size="sm"
              variant="outline"
              nativeButton={false}
              render={
                <a
                  href={publication.article_url}
                  target="_blank"
                  rel="noreferrer"
                />
              }
            >
              <ExternalLink className="h-4 w-4" />
              打开文章
            </Button>
          )}
          {canReconcile && (
            <Button
              size="sm"
              variant="outline"
              onClick={onReconcile}
              disabled={pending}
            >
              <RefreshCw
                className={pending ? "h-4 w-4 animate-spin" : "h-4 w-4"}
              />
              立即检测
            </Button>
          )}
          {canPublish && (
            <Button size="sm" onClick={onPublish} disabled={pending}>
              <Send className="h-4 w-4" />正式发布
            </Button>
          )}
          {canRetryPublish && (
            <Button size="sm" onClick={onRetryPublish} disabled={pending}>
              <RefreshCw className={pending ? "h-4 w-4 animate-spin" : "h-4 w-4"} />
              {hasSubmissionEvidence ? "重新检测发布结果" : "重新尝试正式发布"}
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-2 text-sm sm:grid-cols-2">
          <span>标题：{publication.draft_title || "未提供"}</span>
          <span>草稿 ID：{publication.draft_media_id || "处理中"}</span>
          {publication.publish_id && (
            <span>发布 ID：{publication.publish_id}</span>
          )}
          {publication.msg_id && <span>消息 ID：{publication.msg_id}</span>}
        </div>
        {status === "unsupported" && publication.wechat_status_code === 48001 ? (
          <div className="space-y-1 text-sm text-destructive" role="alert">
            <p className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              {publication.draft_media_id
                ? "微信返回 48001：当前公众号没有“发布草稿”接口权限，无法通过 API 正式发布。"
                : "微信返回 48001：当前公众号没有草稿接口权限。"}
            </p>
            <p className="pl-6 text-xs text-muted-foreground">
              {publication.draft_media_id
                ? "草稿已保留，请前往公众号后台手动发布。这是账号接口权限限制，不是 AppSecret 或 IP 白名单错误；可在微信开发者平台“接口管理 / 接口权限与额度 / 发布能力”中确认，权限开通后再重新尝试。"
                : "请在微信开发者平台“接口管理 / 接口权限与额度 / 草稿管理”中确认；权限开通后再重新尝试。"}
            </p>
          </div>
        ) : publication.last_error && (
          <p className="flex items-start gap-2 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            {publication.last_error}
          </p>
        )}
        {status === "drafted" && (
          <p className="text-xs text-muted-foreground">
            {mode === "manual"
              ? "你可以在这里正式发布，也可以去公众号后台发布；系统会自动识别并开始 30 天数据追踪。"
              : "草稿已就绪，系统正在自动提交正式发布。"}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function CandidatePicker({
  publication,
  pending,
  onSelect,
}: {
  publication: WechatPublication;
  pending: boolean;
  onSelect: (id: string) => void;
}) {
  const candidates = publication.candidates ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>选择对应的已发布文章</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {candidates.map((candidate) => (
          <button
            key={candidate.article_id}
            type="button"
            className="flex w-full items-start justify-between gap-3 rounded-md border border-border p-3 text-left hover:bg-accent disabled:opacity-50"
            disabled={pending}
            onClick={() => onSelect(candidate.article_id)}
          >
            <span>
              <span className="block text-sm font-medium">
                {candidate.title || candidate.article_id}
              </span>
              {candidate.digest && (
                <span className="mt-1 block text-xs text-muted-foreground">
                  {candidate.digest}
                </span>
              )}
            </span>
            <CheckCircle2 className="h-4 w-4 shrink-0 text-muted-foreground" />
          </button>
        ))}
        {candidates.length === 0 && (
          <p className="text-sm text-muted-foreground">
            暂无可选文章，请稍后再次检测。
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function WechatAnalyticsContent({ taskId }: { taskId: string }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["wechat-analytics", taskId],
    queryFn: () => api.wechatAnalytics.getByTask(taskId),
    enabled: Boolean(taskId),
  });
  if (isLoading)
    return (
      <Card>
        <CardContent className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          数据加载中
        </CardContent>
      </Card>
    );
  if (isError || !data?.tracking)
    return (
      <Card>
        <CardContent className="text-sm text-muted-foreground">
          文章已识别，微信数据将在次日可查询后显示。
        </CardContent>
      </Card>
    );
  return <AnalyticsContent analytics={data} />;
}

function AnalyticsContent({ analytics }: { analytics: WechatAnalytics }) {
  const tracking = analytics.tracking!;
  const latest = analytics.metrics ?? analytics.latest;
  const trend = analytics.trend ?? analytics.series ?? [];
  const chartData = useMemo(
    () =>
      trend.map((item) => ({
        date:
          item.stat_date ||
          new Date(item.captured_at).toLocaleDateString("zh-CN", {
            month: "numeric",
            day: "numeric",
          }),
        reads: item.read_users ?? item.int_page_read_user ?? 0,
        shares: item.share_users ?? item.share_count ?? 0,
        favorites: item.collection_users ?? item.add_to_fav_count ?? 0,
      })),
    [trend],
  );
  return (
    <Card>
      <CardHeader className="gap-2 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>公众号数据追踪</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge
              variant={
                tracking.status === "failed"
                  ? "destructive"
                  : tracking.status === "tracking"
                    ? "default"
                    : "outline"
              }
            >
              {tracking.status === "waiting_data"
                ? "等待微信次日数据"
                : tracking.status === "tracking"
                  ? "正在每日采集官方数据"
                  : tracking.status}
            </Badge>
            <span className="text-xs text-muted-foreground">
              追踪至发表后 30 天
            </span>
          </div>
        </div>
        {tracking.article_url && (
          <Button
            size="sm"
            variant="outline"
            nativeButton={false}
            render={
              <a href={tracking.article_url} target="_blank" rel="noreferrer" />
            }
          >
            <ExternalLink className="h-4 w-4" />
            打开文章
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Metric
            icon={<Eye />}
            label="阅读人数"
            value={latest?.read_users ?? latest?.int_page_read_user}
          />
          <Metric
            icon={<Share2 />}
            label="分享人数"
            value={latest?.share_users ?? latest?.share_count}
          />
          <Metric
            icon={<Bookmark />}
            label="收藏人数"
            value={latest?.collection_users ?? latest?.add_to_fav_count}
          />
          <Metric icon={<Star />} label="点赞" value={latest?.like_users} />
          <Metric
            icon={<UserRound />}
            label="在看"
            value={latest?.zaikan_users}
          />
          <Metric
            icon={<MessageCircle />}
            label="评论"
            value={latest?.comment_count}
          />
          <Metric
            icon={<CheckCircle2 />}
            label="完读率"
            value={latest?.read_finish_rate}
            suffix="%"
          />
          <Metric
            icon={<ClockIcon />}
            label="平均阅读时长"
            value={latest?.average_read_active_time}
            suffix="秒"
          />
          <Metric
            icon={<UserPlus />}
            label="阅读后关注"
            value={latest?.read_to_subscribe_users}
          />
        </div>
        {chartData.length > 0 && (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart
                data={chartData}
                margin={{ top: 8, right: 12, left: -18, bottom: 0 }}
              >
                <CartesianGrid
                  strokeDasharray="3 3"
                  className="stroke-border"
                />
                <XAxis
                  dataKey="date"
                  tick={{ fontSize: 12 }}
                  stroke="var(--muted-foreground)"
                />
                <YAxis
                  allowDecimals={false}
                  tick={{ fontSize: 12 }}
                  stroke="var(--muted-foreground)"
                />
                <Tooltip />
                <Line
                  type="monotone"
                  dataKey="reads"
                  name="阅读人数"
                  stroke="#0ea5e9"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="shares"
                  name="分享人数"
                  stroke="#22c55e"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="favorites"
                  name="收藏人数"
                  stroke="#f59e0b"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
function ClockIcon() {
  return <Timer className="h-4 w-4" />;
}
function Metric({
  icon,
  label,
  value,
  suffix = "",
}: {
  icon: ReactNode;
  label: string;
  value?: number;
  suffix?: string;
}) {
  return (
    <div className="rounded-md border border-border bg-muted/20 p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <div className="mt-2 text-xl font-semibold tabular-nums">
        {value == null ? "--" : `${value}${suffix}`}
      </div>
    </div>
  );
}
