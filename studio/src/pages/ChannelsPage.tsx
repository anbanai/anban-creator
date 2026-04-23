import { useState, useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Loader2, Inbox, ChevronDown } from 'lucide-react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import type { Channel, ChannelStats, CreateChannelRequest, PlatformConfig } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { ChannelCard } from '@/components/ChannelCard'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { FileUpload } from '@/components/ui/FileUpload'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { channelSchema, type ChannelFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'

const platformOptions = [
  { value: 'rednote', label: '小红书' },
  { value: 'article', label: '公众号' },
  { value: 'xls', label: '小绿书' },
]

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '活跃', value: 'active' },
  { label: '已归档', value: 'archived' },
]

const styleOptions = [
  { value: '', label: '不设置' },
  { value: 'casual-science', label: '轻松科普风格' },
  { value: 'dan-koe', label: 'Dan Koe 风格' },
  { value: 'cultural-depth', label: '深度文化风格' },
]

const themeOptions = [
  { value: '', label: '不设置' },
  { value: 'autumn-warm', label: '秋日暖光' },
  { value: 'spring-fresh', label: '春日清新' },
  { value: 'ocean-calm', label: '深海静谧' },
]

const CHANNEL_FORM_DEFAULTS: ChannelFormValues = {
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
  image_ratio: '',
  max_concurrent_tasks: 2,
}

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
    image_ratio: (ch.image_ratio as '' | '3:4' | '1:1' | '4:3' | '16:9') || '',
    max_concurrent_tasks: ch.max_concurrent_tasks || CHANNEL_FORM_DEFAULTS.max_concurrent_tasks,
  }
}

export default function ChannelsPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('all')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingChannel, setEditingChannel] = useState<Channel | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)

  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelSchema),
    defaultValues: CHANNEL_FORM_DEFAULTS,
  })

  const selectedPlatform = useWatch({ control: form.control, name: 'platform' })
  const profileUrl = useWatch({ control: form.control, name: 'profile_url' })

  // Auto-focus profile_url field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('profile_url'), 100)
    }
  }, [modalOpen, form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  const { data: platformConfigs } = useQuery({
    queryKey: ['platform-configs'],
    queryFn: () => api.channels.platformConfigs(),
    staleTime: Infinity,
  })

  const platformConfigMap = useMemo(() => {
    const map: Record<string, PlatformConfig> = {}
    if (platformConfigs) {
      for (const pc of platformConfigs) {
        map[pc.id] = pc
      }
    }
    return map
  }, [platformConfigs])

  const currentPlatformConfig = platformConfigMap[selectedPlatform]

  useEffect(() => {
    if (!profileUrl || !selectedPlatform) return
    const pc = platformConfigMap[selectedPlatform]
    if (!pc?.supports_auto_fetch) return
    const timer = setTimeout(() => {
      handleFetchProfile(profileUrl)
    }, 800)
    return () => clearTimeout(timer)
  }, [profileUrl, selectedPlatform, platformConfigMap])

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

  // Batch fetch channel stats via React Query
  const { data: channelDetails } = useQuery({
    queryKey: ['channel-details', statusFilter],
    queryFn: async () => {
      if (!channels || channels.length === 0) return {} as Record<string, ChannelStats>
      const entries = await Promise.all(
        channels.map(async (ch) => {
          try {
            const detail = await api.channels.get(ch.id)
            return [ch.id, detail.stats] as const
          } catch {
            return [ch.id, undefined] as const
          }
        })
      )
      return Object.fromEntries(entries.filter((e): e is [string, ChannelStats] => !!e[1])) as Record<string, ChannelStats>
    },
    enabled: !!channels && channels.length > 0,
  })

  const channelStats = channelDetails ?? {}

  const createMutation = useMutation({
    mutationFn: (data: CreateChannelRequest) => api.channels.create(data),
    onSuccess: () => {
      toast.success('频道创建成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-details'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建频道失败，请重试'))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<CreateChannelRequest> }) =>
      api.channels.update(id, data),
    onSuccess: () => {
      toast.success('频道更新成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-details'] })
      resetModal()
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
      queryClient.invalidateQueries({ queryKey: ['channel-details'] })
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.channels.restore(id),
    onSuccess: () => {
      toast.success('频道已恢复')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-details'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.channels.delete(id),
    onSuccess: () => {
      toast.success('频道已删除')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-details'] })
      setDeleteTarget(null)
    },
  })

  function openCreate() {
    setEditingChannel(null)
    setAdvancedOpen(false)
    form.reset(CHANNEL_FORM_DEFAULTS)
    setModalOpen(true)
  }

  function openEdit(channel: Channel) {
    setEditingChannel(channel)
    setAdvancedOpen(false)
    form.reset(channelToForm(channel))
    setModalOpen(true)
  }

  function closeModal() {
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetModal()
  }

  function resetModal() {
    setModalOpen(false)
    setEditingChannel(null)
    setAdvancedOpen(false)
    form.reset(CHANNEL_FORM_DEFAULTS)
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
      image_ratio: values.image_ratio || undefined,
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
  const isRednote = selectedPlatform === 'rednote'

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
            <form id="channel-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto p-1">
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
                          <SelectItem key={opt.value} value={opt.value} label={opt.label}>{opt.label}</SelectItem>
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
                        <TagInput
                          value={field.value}
                          onChange={field.onChange}
                          placeholder="输入后按回车添加标签"
                        />
                      </FormControl>
                      <FormDescription>按回车或逗号添加标签，用于内容生成</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="style" render={({ field }) => (
                    <FormItem>
                      <FormLabel>{isRednote ? '视觉风格' : '写作风格'}</FormLabel>
                      {isRednote ? (
                        <FormControl>
                          <Textarea
                            placeholder="描述你想要的图片视觉风格，如：手绘感，暖色调，小清新，治愈系水彩插画风格"
                            {...field}
                          />
                        </FormControl>
                      ) : (
                        <Select value={field.value || '_none'} onValueChange={(v) => field.onChange(v === '_none' ? '' : v)}>
                          <FormControl>
                            <SelectTrigger className="w-full">
                              <SelectValue placeholder="选择写作风格" />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            {styleOptions.map((opt) => (
                              <SelectItem key={opt.value || '_none'} value={opt.value || '_none'} label={opt.label}>{opt.label}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                      <FormDescription>{isRednote ? '描述 AI 生成图片的视觉风格，将用于封面和内容图的风格提示' : '选择内置写作风格模板'}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  {!isRednote && <FormField control={form.control} name="theme" render={({ field }) => (
                    <FormItem>
                      <FormLabel>主题</FormLabel>
                      <Select value={field.value || '_none'} onValueChange={(v) => field.onChange(v === '_none' ? '' : v)}>
                        <FormControl>
                          <SelectTrigger className="w-full">
                            <SelectValue placeholder="选择转换主题" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          {themeOptions.map((opt) => (
                            <SelectItem key={opt.value || '_none'} value={opt.value || '_none'} label={opt.label}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <FormDescription>选择内置转换主题模板</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />}

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
                        <FileUpload
                          value={field.value}
                          onChange={field.onChange}
                        />
                      </FormControl>
                      <FormDescription>用于 AI 图片生成的视觉风格参考，保持品牌一致性</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="image_ratio" render={({ field }) => (
                    <FormItem>
                      <FormLabel>图片比例</FormLabel>
                      <Select value={field.value || '_default'} onValueChange={(v) => field.onChange(v === '_default' ? '' : v)}>
                        <FormControl>
                          <SelectTrigger className="w-full">
                            <SelectValue placeholder="跟随平台默认" />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent>
                          <SelectItem value="_default">跟随平台默认</SelectItem>
                          <SelectItem value="3:4">3:4 竖版</SelectItem>
                          <SelectItem value="1:1">1:1 方形</SelectItem>
                          <SelectItem value="4:3">4:3 横版</SelectItem>
                          <SelectItem value="16:9">16:9 宽屏</SelectItem>
                        </SelectContent>
                      </Select>
                      <FormDescription>AI 生成图片的宽高比，封面和内容图统一使用此比例</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )} />

                  <FormField control={form.control} name="max_concurrent_tasks" render={() => (
                    <FormItem>
                      <FormLabel>最大并发任务数</FormLabel>
                      <div className="flex items-center gap-2">
                        <Badge variant="secondary">
                          由账号等级决定
                        </Badge>
                      </div>
                      <FormDescription>并发数由账号等级决定，升级等级可提高并发上限</FormDescription>
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
            <AlertDialogAction variant="destructive" loading={deleteMutation.isPending} onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget) }}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Dirty form confirmation */}
      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃编辑？</AlertDialogTitle>
            <AlertDialogDescription>你有未保存的更改，确定要关闭吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction onClick={resetModal}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
