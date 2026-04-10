import { useState, useEffect, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api, type Channel, type ChannelStats, type CreateChannelRequest, type PlatformConfig, type PlatformProfile } from '@/lib/api'
import { ChannelCard } from '@/components/ChannelCard'
import { Button } from '@/components/ui/Button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/textarea'
import SimpleSelect from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { channelSchema, type ChannelFormValues } from '@/lib/schemas'

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
    },
  })

  const selectedPlatform = form.watch('platform')
  const profileUrl = form.watch('profile_url')

  // Load platform configs
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

  // Auto-fetch profile when profile URL changes (debounced)
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

  // Load stats for each channel
  useEffect(() => {
    if (!channels || channels.length === 0) {
      setChannelStats({})
      return
    }
    const statsMap: Record<string, ChannelStats> = {}
    const promises = channels.map(async (ch) => {
      try {
        const detail = await api.channels.get(ch.id)
        statsMap[ch.id] = detail.stats
      } catch {
        // Ignore stats load failures
      }
      return ch.id
    })
    Promise.all(promises).then(() => {
      setChannelStats(prev => ({ ...prev, ...statsMap }))
    })
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

  // Determine which fields to show based on platform
  const isWechat = selectedPlatform === 'article' || selectedPlatform === 'xls'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-foreground">频道</h1>
          <p className="mt-1 text-sm text-muted-foreground">管理你的内容频道和账号配置。</p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          新建频道
        </Button>
      </div>

      {/* Status filter tabs */}
      <div className="flex gap-1 overflow-x-auto rounded-lg border border-border bg-muted p-1">
        {statusTabs.map((tab) => (
          <button
            key={tab.value}
            onClick={() => setStatusFilter(tab.value)}
            className={`whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              statusFilter === tab.value
                ? 'bg-accent text-accent-foreground'
                : 'text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <svg className="h-8 w-8 animate-spin text-primary" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : !channels || channels.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-border bg-card py-16">
          <svg className="mb-4 h-12 w-12 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <p className="text-sm text-muted-foreground">
            {statusFilter === 'all' ? '还没有频道' : statusFilter === 'active' ? '没有活跃的频道' : '没有已归档的频道'}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">创建你的第一个内容频道开始创作。</p>
        </div>
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

              {/* Platform Selection */}
              <FormField control={form.control} name="platform" render={({ field }) => (
                <FormItem>
                  <FormLabel>平台</FormLabel>
                  <FormControl>
                    <SimpleSelect
                      options={platformOptions}
                      value={field.value}
                      onChange={(e) => {
                        field.onChange(e.target.value)
                        // Clear platform-specific fields when switching
                        form.setValue('wechat_app_id', '')
                        form.setValue('wechat_secret', '')
                      }}
                      disabled={!!editingChannel}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {/* Profile URL (main entry for rednote, optional for wechat) */}
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
                        onClick={() => handleFetchProfile(field.value)}
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

              {/* Basic info */}
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

              {/* Credentials — only for WeChat platforms */}
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

              {/* Advanced settings — collapsible */}
              <div className="border-t border-border pt-2">
                <button
                  type="button"
                  className="flex w-full items-center justify-between py-2 text-sm font-medium text-muted-foreground hover:text-foreground"
                  onClick={() => setAdvancedOpen(!advancedOpen)}
                >
                  高级设置
                  <svg
                    className={`h-4 w-4 transition-transform ${advancedOpen ? 'rotate-180' : ''}`}
                    fill="none" stroke="currentColor" viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
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
            <AlertDialogAction variant="danger" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget) }}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
