import { useState, useEffect, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Loader2, Inbox, ChevronDown } from 'lucide-react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api, type Channel, type ChannelStats, type CreateChannelRequest, type PlatformConfig } from '@/lib/api'
import { ChannelCard } from '@/components/ChannelCard'
import { Button } from '@/components/ui/Button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { channelSchema, type ChannelFormValues } from '@/lib/schemas'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'

const platformOptions = [
  { value: 'article', label: '公众号 (Article)' },
  { value: 'xls', label: '小绿书 (Xiaolvshu)' },
  { value: 'rednote', label: '小红书 (RedNote)' },
]

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '活跃', value: 'active' },
  { label: '已归档', value: 'archived' },
]

function channelToForm(ch: Channel): ChannelFormValues {
  return {
    platform: ch.platform,
    name: ch.name || '',
    profile_url: ch.profile_url || '',
    avatar_url: ch.avatar_url || '',
    wechat_app_id: ch.config?.wechat_app_id || '',
    wechat_secret: '',
    keywords: ch.keywords || '',
    positioning: ch.positioning || '',
    style: ch.style || '',
    theme: ch.theme || '',
    author: ch.author || '',
    reference_image_url: ch.reference_image_url || '',
    max_concurrent_tasks: ch.max_concurrent_tasks || 10,
  }
}

export default function ChannelsPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('all')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingChannel, setEditingChannel] = useState<Channel | null>(null)
  const [channelStats, setChannelStats] = useState<Record<string, ChannelStats>>({})
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelSchema),
    defaultValues: {
      platform: 'article',
      name: '',
      profile_url: '',
      avatar_url: '',
      wechat_app_id: '',
      wechat_secret: '',
      keywords: '',
      positioning: '',
      style: '',
      theme: '',
      author: '',
      reference_image_url: '',
      max_concurrent_tasks: 10,
    },
  })

  const selectedPlatform = form.watch('platform')
  const profileUrl = form.watch('profile_url')

  const { data: platformConfigs } = useQuery({
    queryKey: ['platform-configs'],
    queryFn: () => api.channels.platformConfigs(),
    staleTime: Infinity,
  })

  const platformConfigMap = useCallback(() => {
    const map: Record<string, PlatformConfig> = {}
    if (platformConfigs) {
      for (const pc of platformConfigs) {
        map[pc.id] = pc
      }
    }
    return map
  }, [platformConfigs])

  const currentPlatformConfig = platformConfigMap()[selectedPlatform]

  useEffect(() => {
    if (!profileUrl || !selectedPlatform) return
    const pc = platformConfigMap()[selectedPlatform]
    if (!pc?.supports_auto_fetch) return
    const timer = setTimeout(() => {
      handleFetchProfile(profileUrl)
    }, 800)
    return () => clearTimeout(timer)
  }, [profileUrl, selectedPlatform])

  async function handleFetchProfile(url: string) {
    if (!url || !selectedPlatform) return
    setFetchingProfile(true)
    try {
      const appId = form.getValues('wechat_app_id')
      const secret = form.getValues('wechat_secret')
      const profile = await api.channels.fetchProfile(selectedPlatform, url, appId, secret)
      if (profile.name) form.setValue('name', profile.name)
      if (profile.avatar_url) form.setValue('avatar_url', profile.avatar_url)
      if (profile.positioning) form.setValue('positioning', profile.positioning)
      toast.success('已自动获取账号信息')
    } catch {
      toast.error('获取账号信息失败，请手动填写')
    } finally {
      setFetchingProfile(false)
    }
  }

  const { data: channels, isLoading } = useQuery({
    queryKey: ['channels', statusFilter],
    queryFn: () =>
      api.channels.list({
        status: statusFilter === 'all' ? undefined : statusFilter,
      }),
  })

  useEffect(() => {
    if (!channels || channels.length === 0) {
      setChannelStats({})
      return
    }
    const abortController = new AbortController()
    const statsMap: Record<string, ChannelStats> = {}
    const promises = channels.map(async (ch) => {
      if (abortController.signal.aborted) return ch.id
      try {
        const detail = await api.channels.get(ch.id)
        if (!abortController.signal.aborted) {
          statsMap[ch.id] = detail.stats
        }
      } catch {
        // Ignore stats load failures
      }
      return ch.id
    })
    Promise.all(promises).then(() => {
      if (!abortController.signal.aborted) {
        setChannelStats(prev => ({ ...prev, ...statsMap }))
      }
    })
    return () => abortController.abort()
  }, [channels])

  const createMutation = useMutation({
    mutationFn: (data: CreateChannelRequest) => api.channels.create(data),
    onSuccess: () => {
      toast.success('频道创建成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      closeModal()
    },
    onError: (err: any) => {
      toast.error(err?.response?.data?.msg || '创建频道失败，请重试')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<CreateChannelRequest> }) =>
      api.channels.update(id, data),
    onSuccess: () => {
      toast.success('频道更新成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      closeModal()
    },
    onError: () => {
      toast.error('更新频道失败，请重试')
    },
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.channels.archive(id),
    onSuccess: () => {
      toast.success('频道已归档')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.channels.restore(id),
    onSuccess: () => {
      toast.success('频道已恢复')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.channels.delete(id),
    onSuccess: () => {
      toast.success('频道已删除')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      setDeleteTarget(null)
    },
  })

  function openCreate() {
    setEditingChannel(null)
    setAdvancedOpen(false)
    form.reset({
      platform: 'article',
      name: '',
      profile_url: '',
      avatar_url: '',
      wechat_app_id: '',
      wechat_secret: '',
      keywords: '',
      positioning: '',
      style: '',
      theme: '',
      author: '',
      reference_image_url: '',
      max_concurrent_tasks: 10,
    })
    setModalOpen(true)
  }

  function openEdit(channel: Channel) {
    setEditingChannel(channel)
    setAdvancedOpen(false)
    form.reset(channelToForm(channel))
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingChannel(null)
    form.reset({
      platform: 'article',
      name: '',
      profile_url: '',
      avatar_url: '',
      wechat_app_id: '',
      wechat_secret: '',
      keywords: '',
      positioning: '',
      style: '',
      theme: '',
      author: '',
      reference_image_url: '',
      max_concurrent_tasks: 10,
    })
  }

  async function onSubmit(values: ChannelFormValues) {
    const payload: CreateChannelRequest = {
      platform: values.platform,
      name: values.name?.trim() || undefined,
      profile_url: values.profile_url?.trim() || undefined,
      avatar_url: values.avatar_url?.trim() || undefined,
      positioning: values.positioning?.trim() || undefined,
      keywords: values.keywords?.trim() || undefined,
      style: values.style?.trim() || undefined,
      theme: values.theme?.trim() || undefined,
      author: values.author?.trim() || undefined,
      reference_image_url: values.reference_image_url?.trim() || undefined,
      max_concurrent_tasks: values.max_concurrent_tasks,
      wechat_app_id: values.wechat_app_id?.trim() || undefined,
      wechat_secret: values.wechat_secret?.trim() || undefined,
    }
    if (editingChannel) {
      updateMutation.mutate({ id: editingChannel.id, data: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  function handleDelete(id: string) {
    setDeleteTarget(id)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending
  const isWechat = selectedPlatform === 'article' || selectedPlatform === 'xls'

  return (
    <div className="space-y-6">
      <PageHeader title="频道" description="管理你的内容频道和账号配置。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建频道
        </Button>
      </PageHeader>

      {/* Status filter tabs */}
      <ToggleGroup
        value={[statusFilter]}
        onValueChange={(val) => setStatusFilter(val[0] || 'all')}
        variant="outline"
        size="sm"
        spacing={2}
      >
        {statusTabs.map((tab) => (
          <ToggleGroupItem key={tab.value} value={tab.value}>
            {tab.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      ) : !channels || channels.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title={statusFilter === 'all' ? '还没有频道' : statusFilter === 'active' ? '没有活跃的频道' : '没有已归档的频道'}
          description="创建你的第一个内容频道开始创作。"
          action={{ label: '新建频道', onClick: openCreate }}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              stats={channelStats[channel.id]}
              onEdit={openEdit}
              onArchive={(id) => archiveMutation.mutate(id)}
              onRestore={(id) => restoreMutation.mutate(id)}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingChannel ? '编辑频道' : '新建频道'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="channel-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto pr-1">
              <FormField control={form.control} name="platform" render={({ field }) => (
                <FormItem>
                  <FormLabel>平台</FormLabel>
                  <FormControl>
                    <Select
                      value={field.value}
                      onValueChange={(v) => {
                        field.onChange(v)
                        form.setValue('wechat_app_id', '')
                        form.setValue('wechat_secret', '')
                      }}
                      disabled={!!editingChannel}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择平台" />
                      </SelectTrigger>
                      <SelectContent>
                        {platformOptions.map((opt) => (
                          <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="profile_url" render={({ field }) => (
                <FormItem>
                  <FormLabel>{isWechat ? '平台主页' : '主页链接'}</FormLabel>
                  <div className="flex gap-2">
                    <FormControl>
                      <Input
                        placeholder={isWechat ? 'https://mp.weixin.qq.com/...' : '粘贴小红书主页链接...'}
                        {...field}
                      />
                    </FormControl>
                    {currentPlatformConfig?.supports_auto_fetch && (
                      <Button
                        type="button"
                        variant="secondary"
                        size="sm"
                        loading={fetchingProfile}
                        disabled={!field.value}
                        onClick={() => handleFetchProfile(field.value || '')}
                      >
                        获取
                      </Button>
                    )}
                  </div>
                  {currentPlatformConfig?.supports_auto_fetch && field.value && (
                    <FormDescription>粘贴链接后自动获取账号信息</FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="name" render={({ field }) => (
                <FormItem>
                  <FormLabel>频道名称</FormLabel>
                  <FormControl>
                    <Input placeholder="e.g. 我的科技博客" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="avatar_url" render={({ field }) => (
                <FormItem>
                  <FormLabel>头像</FormLabel>
                  <FormControl>
                    <Input placeholder="自动获取或手动填写 URL" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="positioning" render={({ field }) => (
                <FormItem>
                  <FormLabel>账号定位</FormLabel>
                  <FormControl>
                    <Textarea placeholder="e.g. 面向开发者的实用 AI 教程" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {isWechat && (
                <>
                  <div className="border-t border-border pt-4">
                    <h4 className="mb-3 text-sm font-medium text-muted-foreground">平台凭证</h4>
                  </div>

                  <FormField control={form.control} name="wechat_app_id" render={({ field }) => (
                    <FormItem>
                      <FormLabel>WeChat App ID</FormLabel>
                      <FormControl>
                        <Input placeholder="wx..." {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )} />

                  {!editingChannel && (
                    <FormField control={form.control} name="wechat_secret" render={({ field }) => (
                      <FormItem>
                        <FormLabel>WeChat App Secret</FormLabel>
                        <FormControl>
                          <Input type="password" placeholder="创建后不可查看" {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                  )}
                </>
              )}

              <div className="border-t border-border pt-2">
                <button
                  type="button"
                  className="flex w-full items-center justify-between py-2 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
                  onClick={() => setAdvancedOpen(!advancedOpen)}
                >
                  高级设置
                  <ChevronDown className={`h-4 w-4 transition-transform duration-150 ${advancedOpen ? 'rotate-180' : ''}`} />
                </button>
              </div>

              {advancedOpen && (
                <>
                  <FormField control={form.control} name="keywords" render={({ field }) => (
                    <FormItem>
                      <FormLabel>关键词</FormLabel>
                      <FormControl>
                        <Textarea placeholder="e.g. 科技, AI, 软件工程" {...field} />
                      </FormControl>
                      <FormDescription>逗号分隔的关键词，用于内容生成</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="style" render={({ field }) => (
                    <FormItem>
                      <FormLabel>写作风格</FormLabel>
                      <FormControl>
                        <Input placeholder="e.g. casual-science, dan-koe" {...field} />
                      </FormControl>
                      <FormDescription>内置风格: casual-science, dan-koe, cultural-depth</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="theme" render={({ field }) => (
                    <FormItem>
                      <FormLabel>主题</FormLabel>
                      <FormControl>
                        <Input placeholder="e.g. autumn-warm, spring-fresh" {...field} />
                      </FormControl>
                      <FormDescription>内置主题: autumn-warm, spring-fresh, ocean-calm</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="author" render={({ field }) => (
                    <FormItem>
                      <FormLabel>作者名</FormLabel>
                      <FormControl>
                        <Input placeholder="e.g. 张三" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="reference_image_url" render={({ field }) => (
                    <FormItem>
                      <FormLabel>品牌视觉参考图</FormLabel>
                      <FormControl>
                        <Input placeholder="粘贴图片 URL（支持 JPG, PNG）" {...field} />
                      </FormControl>
                      <FormDescription>用于 AI 图片生成的视觉风格参考，保持品牌一致性</FormDescription>
                      {field.value && (
                        <div className="mt-2">
                          <img
                            src={field.value}
                            alt="参考图预览"
                            className="h-24 w-24 rounded-md object-cover border"
                            onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                          />
                        </div>
                      )}
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="max_concurrent_tasks" render={({ field }) => (
                    <FormItem>
                      <FormLabel>最大并发任务数</FormLabel>
                      <FormControl>
                        <Input type="number" min={1} max={100} placeholder="默认 10" {...field} onChange={(e) => field.onChange(e.target.value ? parseInt(e.target.value) : undefined)} />
                      </FormControl>
                      <FormDescription>同一时间最多可执行的任务数量，超出部分自动排队</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />
                </>
              )}
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="channel-form" loading={isSubmitting}>
              {editingChannel ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => { if (!v) setDeleteTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定要删除此频道吗？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。删除后频道及其所有配置将永久移除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget) }}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
