import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Channel, type ChannelStats, type CreateChannelRequest } from '@/lib/api'
import { ChannelCard } from '@/components/ChannelCard'
import Button from '@/components/ui/Button'
import Modal from '@/components/ui/Modal'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/Input'
import Select from '@/components/ui/Select'

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

interface ChannelFormData {
  platform: string
  name: string
  description: string
  avatar_url: string
  wechat_app_id: string
  wechat_secret: string
  keywords: string
  positioning: string
  style: string
  theme: string
  author: string
  image_api_config: string
}

const emptyForm: ChannelFormData = {
  platform: 'article',
  name: '',
  description: '',
  avatar_url: '',
  wechat_app_id: '',
  wechat_secret: '',
  keywords: '',
  positioning: '',
  style: '',
  theme: '',
  author: '',
  image_api_config: '',
}

function channelToForm(ch: Channel): ChannelFormData {
  return {
    platform: ch.platform,
    name: ch.name,
    description: ch.description || '',
    avatar_url: ch.avatar_url || '',
    wechat_app_id: ch.wechat_app_id || '',
    wechat_secret: '',
    keywords: ch.keywords || '',
    positioning: ch.positioning || '',
    style: ch.style || '',
    theme: ch.theme || '',
    author: ch.author || '',
    image_api_config: ch.image_api_config || '',
  }
}

export default function ChannelsPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('all')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingChannel, setEditingChannel] = useState<Channel | null>(null)
  const [form, setForm] = useState<ChannelFormData>(emptyForm)
  const [formError, setFormError] = useState('')
  const [channelStats, setChannelStats] = useState<Record<string, ChannelStats>>({})

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
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      closeModal()
    },
    onError: () => {
      setFormError('创建频道失败，请重试。')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<CreateChannelRequest> }) =>
      api.channels.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['channels'] })
      closeModal()
    },
    onError: () => {
      setFormError('更新频道失败，请重试。')
    },
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.channels.archive(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.channels.restore(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.channels.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })

  function openCreate() {
    setEditingChannel(null)
    setForm(emptyForm)
    setFormError('')
    setModalOpen(true)
  }

  function openEdit(channel: Channel) {
    setEditingChannel(channel)
    setForm(channelToForm(channel))
    setFormError('')
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingChannel(null)
    setForm(emptyForm)
    setFormError('')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFormError('')

    if (!form.name.trim()) {
      setFormError('频道名称不能为空。')
      return
    }

    // Validate image_api_config JSON if provided
    if (form.image_api_config.trim()) {
      try {
        JSON.parse(form.image_api_config)
      } catch {
        setFormError('Image API Config 不是有效的 JSON。')
        return
      }
    }

    const payload: CreateChannelRequest = {
      platform: form.platform,
      name: form.name.trim(),
      description: form.description.trim() || undefined,
      avatar_url: form.avatar_url.trim() || undefined,
      wechat_app_id: form.wechat_app_id.trim() || undefined,
      wechat_secret: form.wechat_secret.trim() || undefined,
      keywords: form.keywords.trim() || undefined,
      positioning: form.positioning.trim() || undefined,
      style: form.style.trim() || undefined,
      theme: form.theme.trim() || undefined,
      author: form.author.trim() || undefined,
      image_api_config: form.image_api_config.trim() || undefined,
    }

    if (editingChannel) {
      updateMutation.mutate({ id: editingChannel.id, data: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  function handleDelete(id: string) {
    if (window.confirm('确定要删除此频道吗？此操作不可撤销。')) {
      deleteMutation.mutate(id)
    }
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">频道</h1>
          <p className="mt-1 text-sm text-gray-400">管理你的内容频道和账号配置。</p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          新建频道
        </Button>
      </div>

      {/* Status filter tabs */}
      <div className="flex gap-1 overflow-x-auto rounded-lg border border-gray-700 bg-gray-800 p-1">
        {statusTabs.map((tab) => (
          <button
            key={tab.value}
            onClick={() => setStatusFilter(tab.value)}
            className={`whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              statusFilter === tab.value
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:bg-gray-700/50 hover:text-gray-200'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <svg className="h-8 w-8 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : !channels || channels.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-gray-700 bg-gray-800 py-16">
          <svg className="mb-4 h-12 w-12 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <p className="text-sm text-gray-400">
            {statusFilter === 'all' ? '还没有频道' : statusFilter === 'active' ? '没有活跃的频道' : '没有已归档的频道'}
          </p>
          <p className="mt-1 text-xs text-gray-500">创建你的第一个内容频道开始创作。</p>
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

      {/* Create/Edit Modal */}
      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={editingChannel ? '编辑频道' : '新建频道'}
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button onClick={handleSubmit} loading={isSubmitting}>
              {editingChannel ? '更新' : '创建'}
            </Button>
          </>
        }
      >
        <form onSubmit={handleSubmit} className="max-h-[60vh] space-y-4 overflow-y-auto pr-1">
          <Select
            label="平台"
            options={platformOptions}
            value={form.platform}
            onChange={(e) => setForm({ ...form, platform: e.target.value })}
            disabled={!!editingChannel}
          />

          <Input
            label="频道名称"
            placeholder="e.g. 我的科技博客"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            required
          />

          <Textarea
            label="简介"
            placeholder="可选的频道描述"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />

          <Input
            label="头像 URL"
            placeholder="https://example.com/avatar.jpg"
            value={form.avatar_url}
            onChange={(e) => setForm({ ...form, avatar_url: e.target.value })}
          />

          <Input
            label="WeChat App ID"
            placeholder="wx..."
            value={form.wechat_app_id}
            onChange={(e) => setForm({ ...form, wechat_app_id: e.target.value })}
          />

          {!editingChannel && (
            <Input
              label="WeChat App Secret"
              type="password"
              placeholder="创建后不可查看"
              value={form.wechat_secret}
              onChange={(e) => setForm({ ...form, wechat_secret: e.target.value })}
            />
          )}

          <Textarea
            label="关键词"
            placeholder="e.g. 科技, AI, 软件工程"
            hint="逗号分隔的关键词，用于内容生成"
            value={form.keywords}
            onChange={(e) => setForm({ ...form, keywords: e.target.value })}
          />

          <Textarea
            label="定位"
            placeholder="e.g. 面向开发者的实用 AI 教程科技博客"
            value={form.positioning}
            onChange={(e) => setForm({ ...form, positioning: e.target.value })}
          />

          <Input
            label="写作风格"
            placeholder="e.g. casual-science, dan-koe"
            hint="内置风格: casual-science, dan-koe, cultural-depth"
            value={form.style}
            onChange={(e) => setForm({ ...form, style: e.target.value })}
          />

          <Input
            label="主题"
            placeholder="e.g. autumn-warm, spring-fresh"
            hint="内置主题: autumn-warm, spring-fresh, ocean-calm"
            value={form.theme}
            onChange={(e) => setForm({ ...form, theme: e.target.value })}
          />

          <Input
            label="作者名"
            placeholder="e.g. 张三"
            value={form.author}
            onChange={(e) => setForm({ ...form, author: e.target.value })}
          />

          <div>
            <label className="mb-1.5 block text-sm font-medium text-gray-300">
              Image API Config (JSON)
            </label>
            <textarea
              className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 font-mono text-xs text-gray-100 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              rows={4}
              placeholder='{"provider": "gemini", "model": "gemini-3-pro-image-preview"}'
              value={form.image_api_config}
              onChange={(e) => setForm({ ...form, image_api_config: e.target.value })}
            />
            <p className="mt-1 text-xs text-gray-500">
              Provider 选项: openai, gemini, openrouter, volcengine
            </p>
          </div>

          {formError && (
            <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
              {formError}
            </div>
          )}
        </form>
      </Modal>
    </div>
  )
}
