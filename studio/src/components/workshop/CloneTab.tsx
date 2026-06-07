import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Copy, Link2, LayoutGrid, Zap, Droplets, Repeat2 } from 'lucide-react'
import { api } from '@/lib/api'
import type { CreateTaskRequest } from '@/types'
import { ChannelSelector } from '@/components/ChannelSelector'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

const cloneDepthOptions = [
  {
    value: 'style',
    label: '风格复刻',
    description: '保留视觉风格和排版，内容完全重写',
    icon: Droplets,
  },
  {
    value: 'medium',
    label: '中度复刻',
    description: '保留结构和核心卖点，适度改写',
    icon: Repeat2,
  },
  {
    value: 'tight',
    label: '深度复刻',
    description: '高度还原原文风格和表达方式',
    icon: Zap,
  },
]

export default function CloneTab() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  // Source
  const [sourceUrl, setSourceUrl] = useState('')
  const [templateId] = useState(searchParams.get('templateId') || '')
  const [sourceMode, setSourceMode] = useState<'url' | 'template'>(
    searchParams.get('templateId') ? 'template' : 'url'
  )

  // Clone depth
  const [cloneDepth, setCloneDepth] = useState('style')

  // Channel
  const [channelId, setChannelId] = useState('')
  const [channelPlatform, setChannelPlatform] = useState('')

  const createTaskMutation = useMutation({
    mutationFn: (data: CreateTaskRequest) => api.tasks.create(data),
    onSuccess: (task) => {
      toast.success('复刻任务已创建')
      navigate(`/tasks/${task.id}`)
    },
    onError: () => {
      toast.error('创建复刻任务失败，请重试')
    },
  })

  function handleStartClone() {
    if (sourceMode === 'url' && !sourceUrl.trim()) {
      toast.error('请输入笔记链接')
      return
    }
    if (!channelId) {
      toast.error('请选择目标账号')
      return
    }

    const promptParts: string[] = []
    if (sourceMode === 'url') {
      promptParts.push(`复刻笔记: ${sourceUrl.trim()}`)
    } else if (templateId) {
      promptParts.push(`使用模板: ${templateId}`)
    }
    promptParts.push(`复刻深度: ${cloneDepth}`)

    createTaskMutation.mutate({
      type: (channelPlatform as 'seednote' | 'article') || 'seednote',
      channel_id: channelId,
      prompt: promptParts.join('\n'),
    })
  }

  return (
    <div className="space-y-6">
      {/* Source Selection */}
      <Card>
        <CardHeader>
          <CardTitle>选择来源</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex gap-2">
            <Button
              variant={sourceMode === 'url' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setSourceMode('url')}
            >
              <Link2 className="h-4 w-4" />
              笔记链接
            </Button>
            <Button
              variant={sourceMode === 'template' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setSourceMode('template')}
            >
              <LayoutGrid className="h-4 w-4" />
              模板库
            </Button>
          </div>

          {sourceMode === 'url' ? (
            <Input
              value={sourceUrl}
              onChange={(e) => setSourceUrl(e.target.value)}
              placeholder="粘贴种草笔记链接"
            />
          ) : (
            <Button
              variant="outline"
              onClick={() => navigate('/templates')}
              className="w-full"
            >
              <LayoutGrid className="h-4 w-4" />
              浏览模板库选择模板
            </Button>
          )}
        </CardContent>
      </Card>

      {/* Clone Depth */}
      <Card>
        <CardHeader>
          <CardTitle>复刻深度</CardTitle>
        </CardHeader>
        <CardContent>
          <RadioGroup value={cloneDepth} onValueChange={setCloneDepth} className="space-y-3">
            {cloneDepthOptions.map((option) => (
              <label
                key={option.value}
                className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors ${
                  cloneDepth === option.value
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:border-foreground/20'
                }`}
              >
                <RadioGroupItem value={option.value} className="mt-0.5" />
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <option.icon className="h-4 w-4 text-muted-foreground" />
                    <span className="text-sm font-medium text-foreground">{option.label}</span>
                  </div>
                  <p className="mt-0.5 text-xs text-muted-foreground">{option.description}</p>
                </div>
              </label>
            ))}
          </RadioGroup>
        </CardContent>
      </Card>

      {/* Channel Selection */}
      <Card>
        <CardHeader>
          <CardTitle>目标账号</CardTitle>
        </CardHeader>
        <CardContent>
          <ChannelSelector
            value={channelId}
            onChange={(id, platform) => {
              setChannelId(id)
              setChannelPlatform(platform)
            }}
          />
        </CardContent>
      </Card>

      {/* Action */}
      <Button
        onClick={handleStartClone}
        disabled={!channelId || (sourceMode === 'url' && !sourceUrl.trim()) || createTaskMutation.isPending}
        size="lg"
        className="w-full"
      >
        <Copy className="h-4 w-4" />
        开始复刻
      </Button>

      {/* Recent clone tasks placeholder */}
      <Card>
        <CardHeader>
          <CardTitle>最近复刻</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">复刻任务创建后将显示在此处。</p>
        </CardContent>
      </Card>
    </div>
  )
}
