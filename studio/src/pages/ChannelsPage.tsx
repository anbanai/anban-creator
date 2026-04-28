import { useState, useEffect, useMemo, useRef } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
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
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { renderPlatformIcon } from '@/lib/PlatformIcon'

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
  enable_publishing: false,
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
    enable_publishing: ch.config?.enable_publishing ?? false,
  }
}

export default function ChannelsPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('all')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingChannel, setEditingChannel] = useState<Channel | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [profileFetchHint, setProfileFetchHint] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const { submit } = useSubmitLock()

  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelSchema) as Resolver<ChannelFormValues>,
    defaultValues: CHANNEL_FORM_DEFAULTS,
  })

  const selectedPlatform = useWatch({ control: form.control, name: 'platform' })
  const profileUrl = useWatch({ control: form.control, name: 'profile_url' })
  const enablePublishing = useWatch({ control: form.control, name: 'enable_publishing' })

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

  function isValidProfileUrl(value: string) {
    try {
      const parsed = new URL(value)
      return parsed.protocol === 'http:' || parsed.protocol === 'https:'
    } catch {
      return false
    }
  }

  const skipAutoFetchRef = useRef(false)

  useEffect(() => {
    if (skipAutoFetchRef.current) {
      skipAutoFetchRef.current = false
      return
    }
    if (!modalOpen || !profileUrl || !selectedPlatform || !isValidProfileUrl(profileUrl)) return
    const pc = platformConfigMap[selectedPlatform]
    if (!pc?.supports_auto_fetch) return
    const timer = setTimeout(() => {
      void handleFetchProfile(profileUrl, { silent: true })
    }, 800)
    return () => clearTimeout(timer)
  }, [modalOpen, profileUrl, selectedPlatform, platformConfigMap])

  async function handleFetchProfile(url: string, options?: { silent?: boolean }) {
    if (!url || !selectedPlatform) return
    if (!isValidProfileUrl(url)) {
      setProfileFetchHint('请先输入有效的主页链接')
      return
    }
    setFetchingProfile(true)
    setProfileFetchHint(null)
    try {
      const appId = form.getValues('wechat_app_id')
      const secret = form.getValues('wechat_secret')
      const profile = await api.channels.fetchProfile(selectedPlatform, url, appId, secret)
      if (profile.name) form.setValue('name', profile.name)
      if (profile.avatar_url) form.setValue('avatar_url', profile.avatar_url)
      if (profile.positioning) form.setValue('positioning', profile.positioning)
      setProfileFetchHint('已更新账号信息')
      if (!options?.silent) {
        toast.success('已自动获取账号信息')
      }
    } catch {
      setProfileFetchHint('暂时无法自动获取，请继续手动填写')
      if (!options?.silent) {
        toast.error('获取账号信息失败，请手动填写')
      }
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

  const { data: channelStats = {} } = useQuery({
    queryKey: ['channel-stats', statusFilter, channels?.map((channel) => channel.id).join(',')],
    queryFn: async () => {
      if (!channels || channels.length === 0) return {} as Record<string, ChannelStats>
      return api.channels.stats(channels.map((channel) => channel.id))
    },
    enabled: Boolean(channels && channels.length > 0),
  })

  const createMutation = useMutation({
    mutationFn: (data: CreateChannelRequest) => api.channels.create(data),
    onSuccess: () => {
      toast.success('账号创建成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-stats'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建账号失败，请重试'))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<CreateChannelRequest> }) =>
      api.channels.update(id, data),
    onSuccess: () => {
      toast.success('账号更新成功')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-stats'] })
      resetModal()
    },
    onError: () => {
      toast.error('更新账号失败，请重试')
    },
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.channels.archive(id),
    onSuccess: () => {
      toast.success('账号已归档')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-stats'] })
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.channels.restore(id),
    onSuccess: () => {
      toast.success('账号已恢复')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-stats'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.channels.delete(id),
    onSuccess: () => {
      toast.success('账号已删除')
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      queryClient.invalidateQueries({ queryKey: ['channel-stats'] })
      setDeleteTarget(null)
    },
  })

  function openCreate() {
    setEditingChannel(null)
    setAdvancedOpen(false)
    setProfileFetchHint(null)
    form.reset(CHANNEL_FORM_DEFAULTS)
    setModalOpen(true)
  }

  function openEdit(channel: Channel) {
    setEditingChannel(channel)
    setAdvancedOpen(false)
    setProfileFetchHint(null)
    form.reset(channelToForm(channel))
    skipAutoFetchRef.current = true
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
    setShowDirtyDialog(false)
    setEditingChannel(null)
    setAdvancedOpen(false)
    setProfileFetchHint(null)
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
      enable_publishing: values.enable_publishing || undefined,
    }

    // Auto-set image_ratio based on platform if not specified
    if (!payload.image_ratio) {
      if (payload.platform === 'article') {
        payload.image_ratio = '16:9'
      } else if (payload.platform === 'rednote' || payload.platform === 'xls') {
        payload.image_ratio = '3:4'
      }
    }

    // Disable publishing flag when unchecked (credentials preserved)
    if (!values.enable_publishing) {
      payload.enable_publishing = false
    }
    if (editingChannel) {
      await submit(async () => updateMutation.mutateAsync({ id: editingChannel.id, data: payload }))
    } else {
      await submit(async () => createMutation.mutateAsync(payload))
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
      <PageHeader title="账号" description="管理你的内容账号和发布配置。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建账号
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
          title={statusFilter === 'all' ? '还没有账号' : statusFilter === 'active' ? '没有活跃的账号' : '没有已归档的账号'}
          description="创建你的第一个内容账号开始创作。"
          action={{ label: '新建账号', onClick: openCreate }}
          note="配置好账号后，任务和计划都会自动继承对应的平台参数。"
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              stats={channelStats[channel.id]}
              onEdit={openEdit}
              archiving={archiveMutation.isPending}
              restoring={restoreMutation.isPending}
              onArchive={(id) => submit(async () => archiveMutation.mutateAsync(id))}
              onRestore={(id) => submit(async () => restoreMutation.mutateAsync(id))}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingChannel ? '编辑账号' : '新建账号'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="channel-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto p-1">
              <FormField control={form.control} name="platform" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">平台</FormLabel>
                  <FormControl>
                    <Select
                      value={field.value}
                      onValueChange={(v) => {
                        field.onChange(v)
                        form.setValue('wechat_app_id', '')
                        form.setValue('wechat_secret', '')
                        form.setValue('enable_publishing', false)
                      }}
                      disabled={!!editingChannel}
                    >
                      <SelectTrigger className="w-full">
                        {selectedPlatform ? (
                          <span className="flex items-center gap-1.5">
                            {renderPlatformIcon(selectedPlatform)}
                            {platformOptions.find(o => o.value === selectedPlatform)?.label || selectedPlatform}
                          </span>
                        ) : (
                          <SelectValue placeholder="选择平台" />
                        )}
                      </SelectTrigger>
                      <SelectContent>
                        {platformOptions.map((opt) => (
                          <SelectItem key={opt.value} value={opt.value} label={opt.label}>
                            <span className="flex items-center gap-1.5">
                              {renderPlatformIcon(opt.value)}
                              {opt.label}
                            </span>
                          </SelectItem>
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
                        disabled={!field.value || !isValidProfileUrl(field.value || '')}
                        onClick={() => void handleFetchProfile(field.value || '')}
                      >
                        获取
                      </Button>
                    )}
                  </div>
                  {currentPlatformConfig?.supports_auto_fetch && (
                    <FormDescription>
                      {profileFetchHint || '粘贴有效链接后会自动尝试获取账号信息，也可以手动点击获取。'}
                    </FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="name" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">账号名称</FormLabel>
                  <FormControl>
                    <Input placeholder="例如 我的科技博客" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="avatar_url" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">头像</FormLabel>
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
                    <Textarea placeholder="例如 面向开发者的实用 AI 教程" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {isWechat && (
                <>
                  <div className="border-t border-border pt-4">
                    <h4 className="mb-3 text-sm font-medium text-muted-foreground">发布配置</h4>
                  </div>

                  <FormField
                    control={form.control}
                    name="enable_publishing"
                    render={({ field }) => (
                      <FormItem>
                        <div className="flex items-center gap-2">
                          <FormControl>
                            <input
                              type="checkbox"
                              checked={field.value}
                              onChange={(e) => field.onChange(e.target.checked)}
                              className="h-4 w-4 rounded border-input"
                            />
                          </FormControl>
                          <FormLabel className="!mt-0 font-normal cursor-pointer" onClick={() => field.onChange(!field.value)}>
                            启用自动发布
                          </FormLabel>
                        </div>
                        <FormDescription>
                          {field.value
                            ? '开启后，任务完成后将自动发布到公众号'
                            : '未配置微信凭证，将无法使用自动发布到公众号功能'}
                          {field.value && (
                            <span className="mt-1 block">
                              请前往<a href="https://developers.weixin.qq.com/" target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">微信开发者</a>添加 API IP 白名单：47.108.177.204
                            </span>
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  {enablePublishing && (
                    <>
                      <FormField control={form.control} name="wechat_app_id" render={({ field }) => (
                        <FormItem>
                          <FormLabel>微信 AppID</FormLabel>
                          <FormControl>
                            <Input placeholder="wx..." {...field} />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )} />

                      <FormField control={form.control} name="wechat_secret" render={({ field }) => (
                        <FormItem>
                          <FormLabel>微信 AppSecret</FormLabel>
                          <FormControl>
                            <Input
                              type="password"
                              placeholder={editingChannel ? '留空则保持原有密钥不变' : '创建后不可查看'}
                              {...field}
                            />
                          </FormControl>
                          {editingChannel && (
                            <FormDescription>留空则保持原有密钥不变</FormDescription>
                          )}
                          <FormMessage />
                        </FormItem>
                      )} />
                    </>
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
                    <FormItem className="flex items-center gap-3 space-y-0">
                      <FormLabel className="shrink-0 w-20 text-right">作者名</FormLabel>
                      <FormControl>
                        <Input placeholder="例如 张三" {...field} />
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
            <AlertDialogTitle>确定要删除此账号吗？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。删除后账号及其所有配置将永久移除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" loading={deleteMutation.isPending} onClick={() => { if (deleteTarget) submit(async () => deleteMutation.mutateAsync(deleteTarget)) }}>
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
