import { useEffect, useState } from 'react'
import { Star } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/common/button'
import { Card } from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'

interface TaskFeedbackCardProps {
  taskId: string
  enabled?: boolean
  showHeader?: boolean
}

export function TaskFeedbackCard({ taskId, enabled = true, showHeader = true }: TaskFeedbackCardProps) {
  const queryClient = useQueryClient()
  const [rating, setRating] = useState(0)
  const [content, setContent] = useState('')
  const feedbackQuery = useQuery({
    queryKey: queryKeys.tasks.feedback(taskId),
    queryFn: () => api.feedback.getTask(taskId),
    enabled: Boolean(taskId) && enabled,
  })

  useEffect(() => {
    setRating(feedbackQuery.data?.rating ?? 0)
    setContent(feedbackQuery.data?.content ?? '')
  }, [feedbackQuery.data])

  const saveMutation = useMutation({
    mutationFn: () => api.feedback.saveTask(taskId, { rating, content: content.trim() }),
    onSuccess: (feedback) => {
      queryClient.setQueryData(queryKeys.tasks.feedback(taskId), feedback)
      toast.success('评价已保存')
    },
    onError: (error) => {
      toast.error(getApiErrorMessage(error, '评价保存失败，请稍后重试'))
    },
  })

  return (
    <Card className="border-border">
      {showHeader && (
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">人工评价</h2>
          <p className="mt-1 text-xs text-muted-foreground">请为本次任务产出评分，帮助我们持续改进。</p>
        </div>
      )}
      <div className="space-y-4 p-4">
        {feedbackQuery.isLoading ? (
          <p className="text-sm text-muted-foreground">正在加载评价...</p>
        ) : feedbackQuery.isError ? (
          <p role="alert" className="text-sm text-destructive">暂时无法加载评价，请刷新重试。</p>
        ) : (
          <>
            <div>
              <p className="mb-2 text-sm font-medium text-foreground">整体满意度</p>
              <div className="flex gap-1" role="radiogroup" aria-label="整体满意度">
                {[1, 2, 3, 4, 5].map((value) => (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    aria-checked={rating === value}
                    aria-label={`${value} 星`}
                    className="rounded-md p-1 text-muted-foreground transition-colors hover:text-amber-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
                    onClick={() => setRating(value)}
                  >
                    <Star className={`size-6 ${rating >= value ? 'fill-amber-400 text-amber-500' : ''}`} />
                  </button>
                ))}
              </div>
            </div>
            <Textarea
              value={content}
              onChange={(event) => setContent(event.target.value)}
              placeholder="补充具体意见（可选）"
              maxLength={1000}
              rows={3}
              aria-label="评价意见"
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">{feedbackQuery.data ? '已评价，可随时修改' : '评价只对当前任务生效'}</span>
              <Button size="sm" loading={saveMutation.isPending} disabled={rating === 0} onClick={() => saveMutation.mutate()}>
                {feedbackQuery.data ? '更新评价' : '提交评价'}
              </Button>
            </div>
          </>
        )}
      </div>
    </Card>
  )
}

export default TaskFeedbackCard
