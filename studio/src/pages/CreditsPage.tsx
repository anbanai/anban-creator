import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Loader2 } from 'lucide-react'
import { api, type CreditTransaction } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { formatFullDateTimeCN } from '@/lib/labels'
import PageHeader from '@/components/layout/PageHeader'

const transactionTypeLabel: Record<string, string> = {
  sign_in: '每日签到',
  task_deduct: '任务消耗',
  task_refund: '失败退还',
  admin_grant: '管理员充值',
}

function transactionBadgeVariant(type: string) {
  switch (type) {
    case 'sign_in': return 'success'
    case 'task_deduct': return 'danger'
    case 'task_refund': return 'warning'
    case 'admin_grant': return 'info'
    default: return 'neutral'
  }
}

const PAGE_SIZE = 20

export default function CreditsPage() {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)

  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })

  const { data: signInStatusData } = useQuery({
    queryKey: ['credits', 'signInStatus'],
    queryFn: () => api.credits.signInStatus(),
  })

  const { data: transactionsData, isLoading: transactionsLoading } = useQuery({
    queryKey: ['credits', 'transactions', page],
    queryFn: () => api.credits.transactions({ page, page_size: PAGE_SIZE }),
  })

  const signInMutation = useMutation({
    mutationFn: () => api.credits.signIn(),
    onSuccess: () => {
      toast.success('签到成功，积分 +1024')
      queryClient.invalidateQueries({ queryKey: ['credits'] })
    },
    onError: () => {
      toast.error('签到失败，请重试')
    },
  })

  const balance = balanceData?.balance ?? 0
  const signedInToday = signInStatusData?.signed_in_today ?? false
  const transactions = transactionsData?.items ?? []
  const total = transactionsData?.total ?? 0
  const totalPages = Math.ceil(total / PAGE_SIZE)

  return (
    <div className="space-y-6">
      <PageHeader title="积分" description="管理你的积分余额和交易记录。" />

      {/* Balance card */}
      <Card>
        <CardBody>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-muted-foreground">积分余额</p>
              <p className="mt-1 text-4xl font-bold tracking-tight">
                {balanceLoading ? (
                  <span className="inline-block h-10 w-24 animate-pulse rounded bg-muted" />
                ) : (
                  balance.toLocaleString()
                )}
              </p>
            </div>
            <Button
              onClick={() => signInMutation.mutate()}
              disabled={signedInToday || signInMutation.isPending}
              loading={signInMutation.isPending}
            >
              {signedInToday ? '已签到' : '签到 +1024'}
            </Button>
          </div>
        </CardBody>
      </Card>

      {/* Recharge info */}
      <Card>
        <CardBody>
          <h3 className="text-sm font-semibold text-foreground">充值积分</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            如需充值积分，请联系企业微信客服获取更多积分包。客服将在确认后为您充值。
          </p>
        </CardBody>
      </Card>

      {/* Transaction history */}
      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">交易记录</h2>
        </div>
        {transactionsLoading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
          </div>
        ) : transactions.length === 0 ? (
          <div className="py-12 text-center text-sm text-muted-foreground">
            还没有交易记录
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th className="px-4 py-3 font-medium">类型</th>
                    <th className="px-4 py-3 font-medium">数额</th>
                    <th className="px-4 py-3 font-medium">余额</th>
                    <th className="px-4 py-3 font-medium">描述</th>
                    <th className="px-4 py-3 font-medium">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {transactions.map((tx: CreditTransaction) => (
                    <tr key={tx.id} className="border-b border-border transition-colors duration-150 hover:bg-accent">
                      <td className="px-4 py-3">
                        <Badge variant={transactionBadgeVariant(tx.type)}>
                          {transactionTypeLabel[tx.type] || tx.type}
                        </Badge>
                      </td>
                      <td className={`px-4 py-3 font-medium ${tx.amount > 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                        {tx.amount > 0 ? '+' : ''}{tx.amount.toLocaleString()}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {tx.balance_after.toLocaleString()}
                      </td>
                      <td className="max-w-[200px] truncate px-4 py-3 text-muted-foreground">
                        {tx.description}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                        {formatFullDateTimeCN(tx.created_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {totalPages > 1 && (
              <div className="flex items-center justify-between border-t border-border px-4 py-3">
                <p className="text-xs text-muted-foreground">
                  共 {total} 条记录，第 {page}/{totalPages} 页
                </p>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page <= 1}
                    onClick={() => setPage(page - 1)}
                  >
                    上一页
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page >= totalPages}
                    onClick={() => setPage(page + 1)}
                  >
                    下一页
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  )
}
