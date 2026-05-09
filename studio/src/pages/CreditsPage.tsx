import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { toast } from 'sonner'
import { CreditCard } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import type { CreditTransaction, CreditPricing } from '@/types'
import { Card, CardBody } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import Badge from '@/components/ui/Badge'
import { Pagination } from '@/components/ui/Pagination'
import { AccordionRoot, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import { formatFullDateTimeCN, transactionTypeLabel, operationLabel, taskTypeLabelCN } from '@/lib/labels'
import PageHeader from '@/components/layout/PageHeader'
import { useAuth } from '@/contexts/AuthContext'
import MembershipComparison from '@/components/credits/MembershipComparison'

function transactionBadgeVariant(type: string) {
  switch (type) {
    case 'sign_in': return 'success'
    case 'task_deduct': return 'danger'
    case 'task_refund': return 'warning'
    case 'admin_grant': return 'info'
    case 'image_gen':
    case 'image_upload':
    case 'article_write':
    case 'convert':
    case 'humanize':
    case 'topic_research':
    case 'seo':
    case 'draft_publish':
    case 'outline':
      return 'danger'
    default: return 'neutral'
  }
}

const PAGE_SIZE = 20

export default function CreditsPage() {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [rechargeOpen, setRechargeOpen] = useState(false)
  const { submit } = useSubmitLock()
  const { user } = useAuth()

  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })

  const { data: signInStatusData } = useQuery({
    queryKey: ['credits', 'signInStatus'],
    queryFn: () => api.credits.signInStatus(),
  })

  const { data: transactionsData, isLoading: transactionsLoading, isError: transactionsError, refetch: refetchTransactions } = useQuery({
    queryKey: ['credits', 'transactions', page],
    queryFn: () => api.credits.transactions({ page, page_size: PAGE_SIZE }),
  })

  const { data: pricing } = useQuery({
    queryKey: ['credits', 'pricing'],
    queryFn: () => api.credits.pricing(),
  })

  const signInMutation = useMutation({
    mutationFn: () => api.credits.signIn(),
    onSuccess: () => {
      toast.success(`签到成功，积分 +${dailySignInCredits}`)
      queryClient.invalidateQueries({ queryKey: ['credits'] })
    },
    onError: () => {
      toast.error('签到失败，请重试')
    },
  })

  const dailySignInCredits = pricing?.income.daily_sign_in ?? 1024
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
              onClick={() => submit(async () => signInMutation.mutateAsync())}
              disabled={signedInToday || signInMutation.isPending}
              loading={signInMutation.isPending}
            >
              {signedInToday ? '已签到' : `签到 +${dailySignInCredits}`}
            </Button>
          </div>
        </CardBody>
      </Card>

      {/* Membership comparison */}
      <MembershipComparison currentTier={user?.tier ?? 'free'} />

      {/* Recharge */}
      <Card>
        <CardBody>
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-foreground">充值积分</h3>
            <Button variant="outline" size="sm" onClick={() => setRechargeOpen(true)}>
              <CreditCard className="mr-1.5 h-4 w-4" />
              充值
            </Button>
          </div>
        </CardBody>
      </Card>

      {/* Recharge dialog */}
      <Dialog open={rechargeOpen} onOpenChange={setRechargeOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>充值积分</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col items-center space-y-4 py-2">
            <img
              src="/contact-qr.JPG"
              alt="企微客服二维码"
              className="rounded-lg border border-border"
              width={200}
              height={200}
            />
            <p className="text-sm text-muted-foreground">扫码联系客服充值</p>
            <div className="w-full space-y-2">
              {[
                { tier: '基础', price: '10', credits: '10,000' },
                { tier: '标准', price: '50', credits: '55,000' },
                { tier: '专业', price: '100', credits: '120,000' },
              ].map(({ tier, price, credits }) => (
                <div key={tier} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
                  <span className="font-medium">{tier}</span>
                  <span>
                    <span className="text-foreground">{price} 元</span>
                    <span className="mx-2 text-muted-foreground">=</span>
                    <span className="text-primary font-semibold">{credits} 积分</span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Transaction history */}
      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">交易记录</h2>
        </div>
        {transactionsError ? (
          <QueryErrorState onRetry={() => refetchTransactions()} />
        ) : transactionsLoading ? (
          <div className="divide-y divide-border">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-4 px-4 py-3">
                <Skeleton className="h-5 w-16" />
                <Skeleton className="h-4 w-12" />
                <Skeleton className="h-4 w-12" />
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-4 w-20 ml-auto" />
              </div>
            ))}
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
                <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
              </div>
            )}
          </>
        )}
      </Card>

      {/* Pricing guide */}
      {pricing && <PricingGuide pricing={pricing} />}
    </div>
  )
}

function PricingGuide({ pricing }: { pricing: CreditPricing }) {
  const taskCosts = Object.entries(pricing.task_costs)
  const modelOps = Object.entries(pricing.model_costs)
  const income = pricing.income

  const imageModels = modelOps.find(([op]) => op === 'image_gen')?.[1] ?? {}

  const textOps = modelOps.filter(([op]) => op !== 'image_gen')
  const textModels = [...new Set(textOps.flatMap(([, models]) => Object.keys(models)))]

  return (
    <Card>
      <div className="border-b border-border px-4 py-3">
        <h2 className="text-sm font-semibold text-foreground">计费说明</h2>
      </div>
      <AccordionRoot>
        {/* Task costs */}
        <AccordionItem>
          <AccordionTrigger>任务费（Web 端创建任务，包含所有操作）</AccordionTrigger>
          <AccordionContent>
            <PricingTable
              rows={taskCosts.map(([type, cost]) => ({
                label: taskTypeLabelCN[type] ?? type,
                value: `${cost.toLocaleString()} 积分`,
              }))}
            />
          </AccordionContent>
        </AccordionItem>

        {/* Image model costs */}
        {Object.keys(imageModels).length > 0 && (
          <AccordionItem>
            <AccordionTrigger>图片生成（使用平台模型按次扣费）</AccordionTrigger>
            <AccordionContent>
              <PricingTable
                rows={Object.entries(imageModels).map(([model, cost]) => ({
                  label: model,
                  value: `${cost} 积分/张`,
                }))}
              />
            </AccordionContent>
          </AccordionItem>
        )}

        {/* Text operation costs */}
        {textOps.length > 0 && (
          <AccordionItem>
            <AccordionTrigger>文本操作（使用平台模型按次扣费）</AccordionTrigger>
            <AccordionContent>
              <div className="space-y-4">
                {textModels.map((model) => (
                  <div key={model}>
                    <p className="mb-1.5 text-xs font-medium text-muted-foreground">{model}</p>
                    <PricingTable
                      rows={textOps
                        .filter(([, models]) => models[model] !== undefined)
                        .map(([op, models]) => ({
                          label: operationLabel[op] ?? op,
                          value: `${models[model]} 积分`,
                        }))}
                    />
                  </div>
                ))}
              </div>
            </AccordionContent>
          </AccordionItem>
        )}

        {/* Income & free items */}
        <AccordionItem>
          <AccordionTrigger>积分获取 & 免费项目</AccordionTrigger>
          <AccordionContent>
            <PricingTable
              rows={[
                { label: '每日签到', value: `+${income.daily_sign_in.toLocaleString()}` },
                { label: '注册奖励', value: `+${income.register_bonus.toLocaleString()}` },
                { label: '邀请奖励', value: `+${income.invite_reward.toLocaleString()}` },
              ]}
            />
            <div className="mt-3 rounded-lg border border-dashed border-border p-3 space-y-1 text-sm text-muted-foreground">
              <p>图片上传、草稿发布 → 免费</p>
              <p>使用自己的模型（BYOK）→ 全部免费</p>
            </div>
          </AccordionContent>
        </AccordionItem>
      </AccordionRoot>
    </Card>
  )
}

function PricingTable({ rows }: { rows: { label: string; value: string }[] }) {
  return (
    <div className="overflow-hidden rounded-md">
      <table className="w-full text-sm">
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={row.label}
              className={i % 2 === 0 ? 'bg-muted/50' : 'bg-background'}
            >
              <td className="px-3 py-2 text-muted-foreground">{row.label}</td>
              <td className="px-3 py-2 text-right font-medium text-foreground">{row.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
