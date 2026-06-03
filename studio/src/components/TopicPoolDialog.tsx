import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { Channel, TopicPool } from '@/types'

interface TopicPoolDialogProps {
  channel: Channel
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TopicPoolDialog({ channel, open, onOpenChange }: TopicPoolDialogProps) {
  const [statusFilter, setStatusFilter] = useState<string>('unused')
  const [newTopics, setNewTopics] = useState('')
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.topicPool.list(channel.id, statusFilter),
    queryFn: () => api.topicPool.list(channel.id, { status: statusFilter || undefined }),
    enabled: open,
  })

  const addMutation = useMutation({
    mutationFn: (topics: string[]) => api.topicPool.create(channel.id, { topics }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.topicPool.all(channel.id) })
      setNewTopics('')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.topicPool.delete(channel.id, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.topicPool.all(channel.id) })
    },
  })

  const resetMutation = useMutation({
    mutationFn: (id: number) => api.topicPool.reset(channel.id, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.topicPool.all(channel.id) })
    },
  })

  const handleAdd = () => {
    const topics = newTopics
      .split('\n')
      .map((t) => t.trim())
      .filter((t) => t.length > 0)
    if (topics.length > 0) {
      addMutation.mutate(topics)
    }
  }

  const topics: TopicPool[] = data?.items ?? []
  const total = data?.total ?? 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>选题池 - {channel.name}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Add topics */}
          <div className="space-y-2">
            <Textarea
              placeholder="输入选题，每行一个"
              value={newTopics}
              onChange={(e) => setNewTopics(e.target.value)}
              rows={3}
            />
            <Button
              size="sm"
              onClick={handleAdd}
              disabled={addMutation.isPending || !newTopics.trim()}
            >
              {addMutation.isPending ? '添加中...' : '添加选题'}
            </Button>
          </div>

          {/* Status filter */}
          <ToggleGroup
            value={[statusFilter]}
            onValueChange={(v) => setStatusFilter(v[0] || '')}
            className="justify-start"
          >
            <ToggleGroupItem value="unused">未使用</ToggleGroupItem>
            <ToggleGroupItem value="used">已使用</ToggleGroupItem>
            <ToggleGroupItem value="">全部</ToggleGroupItem>
          </ToggleGroup>

          {/* Topic list */}
          {isLoading ? (
            <div className="py-8 text-center text-sm text-muted-foreground">加载中...</div>
          ) : topics.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted-foreground">
              {statusFilter === 'unused' ? '暂无未使用的选题' : statusFilter === 'used' ? '暂无已使用的选题' : '选题池为空'}
            </div>
          ) : (
            <div className="max-h-[40vh] space-y-2 overflow-y-auto">
              {topics.map((topic) => (
                <div
                  key={topic.id}
                  className="flex items-center justify-between rounded-md border border-border px-3 py-2"
                >
                  <div className="flex-1 space-y-1">
                    <p className="text-sm">{topic.topic}</p>
                    <div className="flex items-center gap-2">
                      <Badge variant={topic.status === 'unused' ? 'default' : 'secondary'} className="text-[10px]">
                        {topic.status === 'unused' ? '未使用' : '已使用'}
                      </Badge>
                      {topic.used_at && (
                        <span className="text-[10px] text-muted-foreground">
                          {new Date(topic.used_at).toLocaleDateString()}
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="flex gap-1">
                    {topic.status === 'used' && (
                      <Button
                        variant="ghost"
                        size="xs"
                        disabled={resetMutation.isPending}
                        onClick={() => resetMutation.mutate(topic.id)}
                      >
                        重置
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="xs"
                      disabled={deleteMutation.isPending}
                      onClick={() => deleteMutation.mutate(topic.id)}
                      className="text-destructive hover:text-destructive"
                    >
                      删除
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}

          <div className="text-xs text-muted-foreground">
            共 {total} 条{statusFilter === 'unused' ? '未使用' : statusFilter === 'used' ? '已使用' : ''}选题
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
