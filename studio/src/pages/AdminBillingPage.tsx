import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BadgeCheck, CircleDollarSign, Clock3, Search, ShieldCheck, UserRound, WalletCards } from 'lucide-react'
import { toast } from 'sonner'
import PageHeader from '@/components/layout/PageHeader'
import QueryErrorState from '@/components/QueryErrorState'
import { SimplePagination } from '@/components/SimplePagination'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { formatFullDateTimeCN } from '@/lib/labels'
import type { AdminBillingUser, AdminTopUpStatus, BillingWalletEventKind } from '@/types'

const PAGE_SIZE = 20

const eventLabels: Record<BillingWalletEventKind, string> = {
  topup: '充值', promotion: '推广奖励', charge: '积分扣费', debt_created: '新增欠费',
  debt_repayment: '补缴欠费', reversal: '费用退回', expiry: '奖励到期',
}

const statusLabels: Record<AdminTopUpStatus, string> = {
  pending: '处理中', succeeded: '成功', failed: '失败', replayed: '重复请求',
}

export default function AdminBillingPage() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [selectedUser, setSelectedUser] = useState<AdminBillingUser | null>(null)
  const [credits, setCredits] = useState('')
  const [sourceID, setSourceID] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [activeTab, setActiveTab] = useState('topups')
  const [page, setPage] = useState(1)
  const [filterUserID, setFilterUserID] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [filterFrom, setFilterFrom] = useState('')
  const [filterTo, setFilterTo] = useState('')
  const [filterSourceID, setFilterSourceID] = useState('')

  const usersQuery = useQuery({
    queryKey: ['admin-billing', 'users', search],
    queryFn: () => api.adminBilling.users({ q: search.trim(), limit: 20 }),
  })
  const topupsQuery = useQuery({
    queryKey: ['admin-billing', 'topups', filterUserID, filterStatus, filterFrom, filterTo, filterSourceID, page],
    queryFn: () => api.adminBilling.topups({
      user_id: filterUserID.trim() || undefined,
      status: filterStatus || undefined,
      from: toRFC3339(filterFrom),
      to: toRFC3339(filterTo, 60_000),
      external_source_id: filterSourceID.trim() || undefined,
      offset: (page - 1) * PAGE_SIZE,
      limit: PAGE_SIZE,
    }),
    enabled: activeTab === 'topups',
  })
  const entriesQuery = useQuery({
    queryKey: ['admin-billing', 'entries', filterUserID, page],
    queryFn: () => api.adminBilling.entries({ user_id: filterUserID || undefined, offset: (page - 1) * PAGE_SIZE, limit: PAGE_SIZE }),
    enabled: activeTab === 'entries',
  })

  const amount = Number(credits)
  const debtRepaid = selectedUser ? Math.min(amount || 0, selectedUser.wallet.debt) : 0
  const paidAdded = Math.max(0, (amount || 0) - debtRepaid)
  const newBalance = selectedUser ? selectedUser.wallet.balance + (amount || 0) : 0
  const valid = Boolean(selectedUser && Number.isSafeInteger(amount) && amount > 0 && sourceID.trim().length > 0)
  const queryUser = useMemo(() => search.trim().toLowerCase(), [search])
  const visibleUsers = (usersQuery.data?.items ?? []).filter((user) =>
    !queryUser || `${user.nickname} ${user.email} ${user.id}`.toLowerCase().includes(queryUser),
  )

  const topUpMutation = useMutation({
    mutationFn: () => api.adminBilling.createTopUp({ user_id: selectedUser!.id, credits: amount, external_source_id: sourceID.trim() }),
    onSuccess: async (result) => {
      setConfirmOpen(false)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin-billing'] }),
        queryClient.invalidateQueries({ queryKey: ['billing', 'wallet'] }),
        queryClient.invalidateQueries({ queryKey: ['billing', 'transactions'] }),
      ])
      const originalStatus = result.operation?.original_status
      if (result.status === 'replayed' && originalStatus === 'failed') {
        toast.error(`该流水对应的原充值未成功：${result.operation?.error_message || '请查看充值记录中的失败原因'}`)
      } else if (result.status === 'replayed' && originalStatus === 'pending') {
        toast.error('该流水的原充值仍在处理中，请稍后刷新充值记录')
      } else if (result.status === 'replayed') {
        toast.success('该外部流水已成功处理，已返回原充值结果')
      } else {
        const paid = result.paid_credits_added ?? amount
        const debt = result.debt_repaid_credits ?? 0
        toast.success(`充值成功，本次 ${amount.toLocaleString()} 积分${debt ? `已补欠费 ${debt.toLocaleString()}，新增余额 ${paid.toLocaleString()}` : '已入账'}`)
      }
      if (result.status !== 'replayed' || originalStatus === 'succeeded') {
        setCredits('')
        setSourceID('')
        setSelectedUser(null)
      }
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin-billing', 'topups'] })
      toast.error('充值未完成，失败原因已记录；请先检查充值记录再重试')
    },
  })

  function changeTab(value: string) {
    setActiveTab(value)
    setPage(1)
  }

  return (
    <div className="space-y-6">
      <PageHeader title="管理员充值" description="为用户手动入账，并在同一处核对充值请求与钱包流水。" />

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(320px,0.82fr)_minmax(0,1.8fr)]">
        <Card className="overflow-hidden">
          <CardHeader className="border-b bg-muted/20">
            <CardTitle className="flex items-center gap-2 text-base"><CircleDollarSign className="size-5 text-primary" />发起充值</CardTitle>
            <CardDescription>先选择账号，再填写积分与客服收款流水号。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5 p-5">
            <div className="space-y-2">
              <label htmlFor="admin-billing-user-search" className="text-sm font-medium">充值对象</label>
              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input id="admin-billing-user-search" aria-label="搜索用户" placeholder="邮箱、昵称或用户 ID" value={search} onChange={(event) => setSearch(event.target.value)} className="pl-9" />
              </div>
              {selectedUser ? (
                <div className="rounded-lg border border-primary/30 bg-primary/5 p-3">
                  <div className="flex items-start gap-3">
                    <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"><UserRound className="size-4" /></span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{selectedUser.nickname || '未设置昵称'}</p>
                      <p className="truncate text-xs text-muted-foreground">{selectedUser.email}</p>
                      <p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{selectedUser.id}</p>
                    </div>
                    <Button variant="ghost" size="sm" onClick={() => setSelectedUser(null)}>更换</Button>
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-primary/10 pt-3 text-xs">
                    <WalletMetric label="可用余额" value={selectedUser.wallet.balance} strong />
                    <WalletMetric label="现金积分" value={selectedUser.wallet.paid} />
                    <WalletMetric label="奖励积分" value={selectedUser.wallet.promotional} />
                    <WalletMetric label="待补欠费" value={selectedUser.wallet.debt} debt={selectedUser.wallet.debt > 0} />
                  </div>
                </div>
              ) : (
                <div className="max-h-52 overflow-y-auto rounded-lg border border-border">
                  {usersQuery.isLoading ? <div className="space-y-2 p-3"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
                    : usersQuery.isError ? <div className="p-3"><QueryErrorState title="用户搜索失败" onRetry={() => void usersQuery.refetch()} /></div>
                      : visibleUsers.length === 0 ? <p className="px-3 py-5 text-center text-sm text-muted-foreground">输入邮箱、昵称或用户 ID 查找账号</p>
                        : visibleUsers.map((user) => (
                          <button key={user.id} type="button" onClick={() => setSelectedUser(user)} aria-label={`选择${user.nickname || user.email}`} className="flex w-full items-center gap-3 border-b border-border px-3 py-2.5 text-left last:border-0 hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                            <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground"><UserRound className="size-4" /></span>
                            <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{user.nickname || '未设置昵称'}</span><span className="block truncate text-xs text-muted-foreground">{user.email}</span></span>
                            <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{user.wallet.balance.toLocaleString()}</span>
                          </button>
                        ))}
                </div>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="admin-billing-credits" className="text-sm font-medium">充值积分</label>
              <div className="relative"><Input id="admin-billing-credits" aria-label="充值积分" type="number" min="1" step="1" inputMode="numeric" placeholder="输入正整数" value={credits} onChange={(event) => setCredits(event.target.value)} className="pr-14 tabular-nums" /><span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">积分</span></div>
            </div>

            <div className="space-y-2">
              <label htmlFor="admin-billing-source" className="text-sm font-medium">外部流水号</label>
              <Input id="admin-billing-source" aria-label="外部流水号" placeholder="如：微信支付流水号" value={sourceID} onChange={(event) => setSourceID(event.target.value)} maxLength={128} />
              <p className="text-xs leading-5 text-muted-foreground">必填，用于防止重复入账。每次充值请使用唯一流水号。</p>
            </div>

            {selectedUser && amount > 0 && Number.isSafeInteger(amount) && (
              <div className="space-y-2 rounded-lg bg-muted/50 px-3.5 py-3 text-sm">
                <p className="font-medium">到账预览</p>
                <div className="flex justify-between text-muted-foreground"><span>本次充值</span><span className="tabular-nums">{amount.toLocaleString()} 积分</span></div>
                {selectedUser.wallet.debt > 0 && <div className="flex justify-between text-muted-foreground"><span>优先补缴欠费</span><span className="tabular-nums">−{debtRepaid.toLocaleString()}</span></div>}
                <div className="flex justify-between text-muted-foreground"><span>新增现金积分</span><span className="tabular-nums">+{paidAdded.toLocaleString()}</span></div>
                <div className="flex justify-between border-t border-border pt-2 font-medium"><span>充值后可用余额</span><span className="tabular-nums">{newBalance.toLocaleString()}</span></div>
              </div>
            )}

            {selectedUser?.wallet.debt ? <p className="flex gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-xs leading-5 text-amber-800 dark:text-amber-300"><Clock3 className="mt-0.5 size-4 shrink-0" />充值会优先补齐 {selectedUser.wallet.debt.toLocaleString()} 积分欠费，剩余部分进入现金积分。</p> : null}

            <Button className="w-full" disabled={!valid || topUpMutation.isPending} loading={topUpMutation.isPending} onClick={() => setConfirmOpen(true)}><ShieldCheck data-icon="inline-start" />确认充值</Button>
            <p className="text-center text-[11px] text-muted-foreground">操作人和充值结果会自动记录；管理员登录态由服务端校验。</p>
          </CardContent>
        </Card>

        <Card className="min-w-0 overflow-hidden">
          <CardHeader className="gap-4 border-b sm:flex-row sm:items-center sm:justify-between">
            <div><CardTitle className="flex items-center gap-2 text-base"><WalletCards className="size-5 text-muted-foreground" />入账与流水</CardTitle><CardDescription className="mt-1">全站记录，选择用户后可聚焦查看。</CardDescription></div>
            <div className="flex flex-wrap items-center gap-2">
              <Input aria-label="按用户 ID 筛选" placeholder="用户 ID" value={filterUserID} onChange={(event) => { setFilterUserID(event.target.value); setPage(1) }} className="h-8 w-36 text-xs" />
              {activeTab === 'topups' && <>
                <select aria-label="按状态筛选" value={filterStatus} onChange={(event) => { setFilterStatus(event.target.value); setPage(1) }} className="h-8 rounded-md border border-input bg-background px-2 text-xs text-foreground">
                  <option value="">全部状态</option><option value="pending">处理中</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="replayed">重复请求</option>
                </select>
                <Input aria-label="开始时间" type="datetime-local" value={filterFrom} onChange={(event) => { setFilterFrom(event.target.value); setPage(1) }} className="h-8 w-44 text-xs" />
                <Input aria-label="结束时间" type="datetime-local" value={filterTo} onChange={(event) => { setFilterTo(event.target.value); setPage(1) }} className="h-8 w-44 text-xs" />
                <Input aria-label="按外部流水号筛选" placeholder="外部流水号" value={filterSourceID} onChange={(event) => { setFilterSourceID(event.target.value); setPage(1) }} className="h-8 w-36 text-xs" />
              </>}
              {(filterUserID || filterStatus || filterFrom || filterTo || filterSourceID) && <Button size="sm" variant="ghost" onClick={() => { setFilterUserID(''); setFilterStatus(''); setFilterFrom(''); setFilterTo(''); setFilterSourceID(''); setPage(1) }}>清除筛选</Button>}
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <Tabs value={activeTab} onValueChange={changeTab}>
              <div className="border-b px-4 pt-3"><TabsList><TabsTrigger value="topups">充值记录</TabsTrigger><TabsTrigger value="entries">钱包流水</TabsTrigger></TabsList></div>
              <TabsContent value="topups" className="m-0">
                {topupsQuery.isError ? <div className="p-5"><QueryErrorState onRetry={() => void topupsQuery.refetch()} /></div>
                  : topupsQuery.isLoading ? <RecordSkeleton />
                    : (topupsQuery.data?.items.length ?? 0) === 0 ? <EmptyRecords title="暂无充值记录" description="成功、失败和重复请求都会显示在这里。" />
                      : <>
                        <div className="overflow-x-auto">
                          <table className="w-full min-w-[760px] text-left text-sm">
                            <thead className="bg-muted/30 text-xs text-muted-foreground"><tr><th className="px-4 py-3 font-medium">用户 / 操作人</th><th className="px-4 py-3 font-medium">充值积分</th><th className="px-4 py-3 font-medium">外部流水号</th><th className="px-4 py-3 font-medium">结果</th><th className="px-4 py-3 font-medium">时间</th></tr></thead>
                            <tbody className="divide-y divide-border">{topupsQuery.data?.items.map((entry) => <tr key={entry.id} className="align-top hover:bg-muted/20"><td className="px-4 py-3"><p className="font-mono text-[11px]">{entry.user_id}</p><p className="mt-1 text-[11px] text-muted-foreground">操作人 {entry.admin_user_id}</p></td><td className="whitespace-nowrap px-4 py-3 font-semibold tabular-nums">{entry.credits.toLocaleString()}</td><td className="max-w-48 break-all px-4 py-3 font-mono text-xs">{entry.external_source_id}</td><td className="px-4 py-3"><StatusBadge status={entry.status} />{entry.error_message && <p className="mt-1 max-w-44 text-[11px] text-destructive">{entry.error_message}</p>}{entry.debt_repaid_credits > 0 && <p className="mt-1 text-[11px] text-muted-foreground">补欠费 {entry.debt_repaid_credits.toLocaleString()}</p>}</td><td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{formatFullDateTimeCN(entry.created_at)}</td></tr>)}</tbody>
                          </table>
                        </div>
                        <Pagination total={topupsQuery.data?.total ?? 0} page={page} onPageChange={setPage} />
                      </>}
              </TabsContent>
              <TabsContent value="entries" className="m-0">
                {entriesQuery.isError ? <div className="p-5"><QueryErrorState onRetry={() => void entriesQuery.refetch()} /></div>
                  : entriesQuery.isLoading ? <RecordSkeleton />
                    : (entriesQuery.data?.items.length ?? 0) === 0 ? <EmptyRecords title="暂无钱包流水" description="充值、欠费补缴、消费和奖励会记录在这里。" />
                      : <>
                        <div className="overflow-x-auto">
                          <table className="w-full min-w-[720px] text-left text-sm">
                            <thead className="bg-muted/30 text-xs text-muted-foreground"><tr><th className="px-4 py-3 font-medium">类型</th><th className="px-4 py-3 font-medium">用户</th><th className="px-4 py-3 font-medium">钱包变化</th><th className="px-4 py-3 font-medium">来源 / 关联</th><th className="px-4 py-3 font-medium">时间</th></tr></thead>
                            <tbody className="divide-y divide-border">{entriesQuery.data?.items.map((entry) => <tr key={entry.id} className="align-top hover:bg-muted/20"><td className="px-4 py-3"><Badge variant={entry.event_kind === 'charge' || entry.event_kind === 'debt_created' ? 'outline' : 'secondary'}>{eventLabels[entry.event_kind]}</Badge></td><td className="px-4 py-3 font-mono text-[11px]">{entry.user_id}</td><td className="px-4 py-3 text-xs tabular-nums"><p>现金 {formatDelta(entry.paid_delta)}</p><p className="mt-1 text-muted-foreground">奖励 {formatDelta(entry.promotional_delta)} · 欠费 {formatDelta(entry.debt_delta)}</p></td><td className="max-w-52 break-all px-4 py-3 text-xs text-muted-foreground">{entry.source_id || (entry.resource_type ? `${entry.resource_type} / ${entry.resource_id}` : '钱包调整')}</td><td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{formatFullDateTimeCN(entry.created_at)}</td></tr>)}</tbody>
                          </table>
                        </div>
                        <Pagination total={entriesQuery.data?.total ?? 0} page={page} onPageChange={setPage} />
                      </>}
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>
      </div>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>确认管理员充值</DialogTitle><DialogDescription>请核对目标账号、积分和外部流水号。确认后会立即写入用户钱包。</DialogDescription></DialogHeader>
          <div className="space-y-3 rounded-lg bg-muted/50 p-4 text-sm">
            <SummaryRow label="充值对象" value={`${selectedUser?.nickname || '用户'} · ${selectedUser?.email || ''}`} />
            <SummaryRow label="充值积分" value={`${amount.toLocaleString()} 积分`} strong />
            {debtRepaid > 0 && <SummaryRow label="优先补缴欠费" value={`${debtRepaid.toLocaleString()} 积分`} />}
            <SummaryRow label="新增现金积分" value={`${paidAdded.toLocaleString()} 积分`} />
            <SummaryRow label="外部流水号" value={sourceID.trim()} />
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setConfirmOpen(false)}>返回修改</Button><Button disabled={topUpMutation.isPending} loading={topUpMutation.isPending} onClick={() => topUpMutation.mutate()}><BadgeCheck data-icon="inline-start" />确认并入账</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function WalletMetric({ label, value, strong = false, debt = false }: { label: string; value: number; strong?: boolean; debt?: boolean }) {
  return <div className="flex justify-between gap-2"><span className="text-muted-foreground">{label}</span><span className={`font-medium tabular-nums ${strong ? 'text-foreground' : ''} ${debt ? 'text-amber-700 dark:text-amber-300' : ''}`}>{value.toLocaleString()}</span></div>
}

function StatusBadge({ status }: { status: AdminTopUpStatus }) {
  const variant = status === 'succeeded' ? 'secondary' : status === 'failed' ? 'destructive' : 'outline'
  return <Badge variant={variant}>{statusLabels[status]}</Badge>
}

function SummaryRow({ label, value, strong = false }: { label: string; value: string; strong?: boolean }) {
  return <div className="flex items-start justify-between gap-4"><span className="shrink-0 text-muted-foreground">{label}</span><span className={`break-all text-right ${strong ? 'font-semibold text-foreground' : ''}`}>{value}</span></div>
}

function EmptyRecords({ title, description }: { title: string; description: string }) {
  return <div className="flex flex-col items-center gap-2 px-6 py-14 text-center"><span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground"><WalletCards className="size-5" /></span><p className="text-sm font-medium">{title}</p><p className="max-w-xs text-xs text-muted-foreground">{description}</p></div>
}

function RecordSkeleton() {
  return <div className="space-y-3 p-4">{Array.from({ length: 5 }, (_, index) => <div key={index} className="flex gap-4"><Skeleton className="h-5 w-28" /><Skeleton className="h-5 w-20" /><Skeleton className="h-5 flex-1" /><Skeleton className="h-5 w-28" /></div>)}</div>
}

function Pagination({ total, page, onPageChange }: { total: number; page: number; onPageChange: (page: number) => void }) {
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  if (totalPages <= 1) return null
  return <div className="flex items-center justify-between gap-4 border-t border-border px-4 py-3"><p className="text-xs text-muted-foreground">共 {total} 条记录</p><SimplePagination page={page} totalPages={totalPages} onPageChange={onPageChange} /></div>
}

function formatDelta(value: number) { return `${value > 0 ? '+' : ''}${value.toLocaleString()}` }

function toRFC3339(value: string, addMilliseconds = 0) {
  return value ? new Date(new Date(value).getTime() + addMilliseconds).toISOString() : undefined
}
