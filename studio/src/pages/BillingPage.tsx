import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, CreditCard, ReceiptText, Users } from 'lucide-react'
import { toast } from 'sonner'
import PageHeader from '@/components/layout/PageHeader'
import QueryErrorState from '@/components/QueryErrorState'
import { SimplePagination } from '@/components/SimplePagination'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { formatFullDateTimeCN } from '@/lib/labels'
import type { BillingTransaction, BillingWalletEventKind } from '@/types'

const PAGE_SIZE = 20

const eventLabels: Record<BillingWalletEventKind, string> = {
  topup: '充值',
  promotion: '推广奖励',
  charge: '固定价扣费',
  debt_created: '新增欠费',
  debt_repayment: '补缴欠费',
  reversal: '费用退回',
  expiry: '奖励到期',
}

function entryBalanceDelta(entry: BillingTransaction) {
  return entry.paid_delta + entry.promotional_delta - entry.debt_delta
}

function entryDescription(entry: BillingTransaction) {
  const resource = [entry.resource_type, entry.resource_id].filter(Boolean).join(' / ')
  if (resource) return resource
  const source = [entry.source_type, entry.source_id].filter(Boolean).join(' / ')
  return source || entry.catalog_id || '钱包调整'
}

function deltaDetails(entry: BillingTransaction) {
  const parts: string[] = []
  if (entry.paid_delta !== 0) parts.push(`现金积分 ${entry.paid_delta > 0 ? '+' : ''}${entry.paid_delta.toLocaleString()}`)
  if (entry.promotional_delta !== 0) parts.push(`奖励积分 ${entry.promotional_delta > 0 ? '+' : ''}${entry.promotional_delta.toLocaleString()}`)
  if (entry.debt_delta !== 0) parts.push(`欠费 ${entry.debt_delta > 0 ? '+' : ''}${entry.debt_delta.toLocaleString()}`)
  return parts.join(' · ')
}

export default function BillingPage() {
  const [page, setPage] = useState(1)
  const [rechargeOpen, setRechargeOpen] = useState(false)
  const offset = (page - 1) * PAGE_SIZE

  const walletQuery = useQuery({
    queryKey: ['billing', 'wallet'],
    queryFn: () => api.billing.wallet(),
  })
  const transactionsQuery = useQuery({
    queryKey: ['billing', 'transactions', page],
    queryFn: () => api.billing.transactions({ offset, limit: PAGE_SIZE }),
  })
  const referralQuery = useQuery({
    queryKey: ['billing', 'referral'],
    queryFn: () => api.billing.referral(),
  })

  const wallet = walletQuery.data
  const transactions = transactionsQuery.data?.items ?? []
  const total = transactionsQuery.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const referral = referralQuery.data

  if (walletQuery.isError) {
    return (
      <div className="space-y-6">
        <PageHeader title="钱包" description="查看可用积分、奖励积分和欠费。" />
        <QueryErrorState onRetry={() => walletQuery.refetch()} />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader title="钱包" description="固定价格在任务或增值操作开始前确定。">
        <Button size="sm" onClick={() => setRechargeOpen(true)}>
          <CreditCard data-icon="inline-start" />
          充值
        </Button>
      </PageHeader>

      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-4">
          {walletQuery.isLoading ? (
            Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-16 w-full" />)
          ) : (
            <>
              <WalletValue label="可用余额" value={wallet?.balance ?? 0} emphasized />
              <WalletValue label="现金积分" value={wallet?.paid ?? 0} />
              <WalletValue label="奖励积分" value={wallet?.promotional ?? 0} />
              <WalletValue label="待补欠费" value={wallet?.debt ?? 0} debt />
            </>
          )}
        </CardContent>
      </Card>

      {(wallet?.debt ?? 0) > 0 && (
        <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm">
          <p className="font-medium text-foreground">当前欠费 {(wallet?.debt ?? 0).toLocaleString()} 积分</p>
          <p className="mt-1 text-xs text-muted-foreground">已接受的任务仍可完成。充值会优先补齐欠费，补齐前不能创建新任务或发起独立增值操作。</p>
        </div>
      )}

      {referral?.program && (
        <Card>
          <CardContent className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex min-w-0 items-start gap-3">
              <Users className="mt-0.5 size-5 shrink-0 text-primary" />
              <div className="min-w-0">
                <p className="text-sm font-semibold text-foreground">邀请好友</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  好友首次充值至少 {referral.program.minimum_topup_credits.toLocaleString()} 积分后，双方各得
                  {' '}{referral.program.invitee_credits.toLocaleString()} 奖励积分。
                </p>
                <p className="mt-1 truncate text-xs text-muted-foreground" title={referral.invite_link}>{referral.invite_link}</p>
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                void navigator.clipboard.writeText(referral.invite_link)
                toast.success('邀请链接已复制')
              }}
            >
              <Copy data-icon="inline-start" />
              复制链接
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <ReceiptText className="size-4 text-muted-foreground" />
          <div>
            <h2 className="text-sm font-semibold text-foreground">钱包流水</h2>
            <p className="mt-0.5 text-xs text-muted-foreground">用户价格只记录固定 SKU，不展示供应商 token 或成本。</p>
          </div>
        </div>
        {transactionsQuery.isError ? (
          <QueryErrorState onRetry={() => transactionsQuery.refetch()} />
        ) : transactionsQuery.isLoading ? (
          <div className="divide-y divide-border">
            {Array.from({ length: 5 }).map((_, index) => (
              <div key={index} className="flex gap-4 px-4 py-3">
                <Skeleton className="h-5 w-20" />
                <Skeleton className="h-5 w-24" />
                <Skeleton className="ml-auto h-5 w-32" />
              </div>
            ))}
          </div>
        ) : transactions.length === 0 ? (
          <div className="py-12 text-center text-sm text-muted-foreground">暂无钱包流水</div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th className="px-4 py-3 font-medium">类型</th>
                    <th className="px-4 py-3 font-medium">余额变化</th>
                    <th className="px-4 py-3 font-medium">分账</th>
                    <th className="px-4 py-3 font-medium">关联对象</th>
                    <th className="px-4 py-3 font-medium">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {transactions.map((entry) => {
                    const delta = entryBalanceDelta(entry)
                    return (
                      <tr key={entry.id} className="border-b border-border hover:bg-accent">
                        <td className="px-4 py-3"><Badge variant="secondary">{eventLabels[entry.event_kind]}</Badge></td>
                        <td className={`px-4 py-3 font-medium ${delta > 0 ? 'text-emerald-600' : delta < 0 ? 'text-destructive' : 'text-muted-foreground'}`}>
                          {delta > 0 ? '+' : ''}{delta.toLocaleString()}
                        </td>
                        <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{deltaDetails(entry)}</td>
                        <td className="max-w-72 truncate px-4 py-3 text-muted-foreground" title={entryDescription(entry)}>{entryDescription(entry)}</td>
                        <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{formatFullDateTimeCN(entry.created_at)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            {totalPages > 1 && (
              <div className="flex items-center justify-between border-t border-border px-4 py-3">
                <p className="text-xs text-muted-foreground">共 {total} 条记录，第 {page}/{totalPages} 页</p>
                <SimplePagination page={page} totalPages={totalPages} onPageChange={setPage} />
              </div>
            )}
          </>
        )}
      </Card>

      <Dialog open={rechargeOpen} onOpenChange={setRechargeOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>联系客服充值</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col items-center gap-4 py-2">
            <img src="/contact-qr.JPG" alt="企微客服二维码" className="rounded-lg border border-border" width={200} height={200} />
            <div className="space-y-1 text-center text-sm text-muted-foreground">
              <p>扫码后提供账号信息，由客服通过充值 API 入账。</p>
              <p>充值不附赠积分；如有欠费，到账积分会优先补齐欠费。</p>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function WalletValue({ label, value, emphasized = false, debt = false }: {
  label: string
  value: number
  emphasized?: boolean
  debt?: boolean
}) {
  return (
    <div className="min-w-0">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`mt-1 truncate font-semibold tabular-nums ${emphasized ? 'text-3xl text-foreground' : 'text-xl'} ${debt && value > 0 ? 'text-amber-600' : ''}`}>
        {value.toLocaleString()}
      </p>
    </div>
  )
}
