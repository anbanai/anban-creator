import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { getApiErrorMessage } from '@/lib/http-client'
import { Card, CardContent } from '@/components/ui/Card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const IMAGE_PROVIDERS = [
  { value: 'openai', label: 'OpenAI (DALL-E)' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'volcengine', label: 'Volcengine/Seedream' },
]

const imageProviderNotes: Record<string, string> = {
  openai: 'OpenAI 仅支持 Images API 兼容接口，不是任意 OpenAI-compatible 聊天接口。',
  gemini: 'Gemini 使用 Google 图片生成接口，请填写支持图片生成的模型。',
  volcengine: 'Volcengine 使用火山方舟 Seedream 图片生成接口。',
}

function ModelSection({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="rounded-lg">
      <button
        className="flex items-center justify-between w-full px-3 py-2.5 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <div>
          <p className="text-sm font-medium text-foreground">{title}</p>
          <p className="text-xs text-muted-foreground mt-0.5">{description}</p>
        </div>
        <svg
          className={`w-4 h-4 text-muted-foreground transition-transform ${expanded ? 'rotate-180' : ''}`}
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {expanded && <div className="px-3 pb-3 space-y-3 border-t border-border pt-3">{children}</div>}
    </div>
  )
}

function MaskedInput({
  label,
  value,
  onChange,
  placeholder,
  hasValue,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  hasValue?: boolean
}) {
  const [show, setShow] = useState(false)
  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <Label className="text-xs">{label}</Label>
        {hasValue && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
      </div>
      <div className="flex items-center gap-2">
        <Input
          type={show ? 'text' : 'password'}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          className="flex-1 text-xs"
        />
        <Button
          size="sm"
          variant="ghost"
          className="shrink-0 text-xs"
          onClick={() => setShow(!show)}
        >
          {show ? '隐藏' : '显示'}
        </Button>
      </div>
    </div>
  )
}

export default function ModelConfigSection() {
  const queryClient = useQueryClient()
  const { data: config, isLoading } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
  })

  const [textModel, setTextModel] = useState('')
  const [textEndpoint, setTextEndpoint] = useState('')
  const [textApiKey, setTextApiKey] = useState('')
  const [textProxy, setTextProxy] = useState('')
  const [imageProvider, setImageProvider] = useState('')
  const [imageModel, setImageModel] = useState('')
  const [imageEndpoint, setImageEndpoint] = useState('')
  const [imageApiKey, setImageApiKey] = useState('')
  const [imageProxy, setImageProxy] = useState('')

  const initialized = useRef(false)

  useEffect(() => {
    if (!config || initialized.current) return
    if (config.text) {
      setTextModel(config.text.model || '')
      setTextEndpoint(config.text.endpoint || '')
      setTextApiKey(config.text.api_key ? '****' : '')
      setTextProxy(config.text.proxy || '')
    }
    if (config.image) {
      setImageProvider(config.image.provider || '')
      setImageModel(config.image.model || '')
      setImageEndpoint(config.image.endpoint || '')
      setImageApiKey(config.image.api_key ? '****' : '')
      setImageProxy(config.image.proxy || '')
    }
    initialized.current = true
  }, [config])

  const textHasConfig = !!(config?.text?.model || config?.text?.endpoint || config?.text?.api_key)
  const imageHasConfig = !!(config?.image?.model || config?.image?.endpoint || config?.image?.api_key || config?.image?.provider)

  const textMutation = useMutation({
    mutationFn: () =>
      api.modelConfig.update({
        text: {
          model: textModel,
          endpoint: textEndpoint,
          api_key: textApiKey === '****' ? '****' : textApiKey,
          proxy: textProxy,
        },
      }),
    onSuccess: () => {
      toast.success('文本模型配置已保存')
      queryClient.invalidateQueries({ queryKey: queryKeys.modelConfig.all })
    },
    onError: (err) => toast.error(getApiErrorMessage(err, '保存失败')),
  })

  const imageMutation = useMutation({
    mutationFn: () =>
      api.modelConfig.update({
        image: {
          provider: imageProvider,
          model: imageModel,
          endpoint: imageEndpoint,
          api_key: imageApiKey === '****' ? '****' : imageApiKey,
          proxy: imageProxy,
        },
      }),
    onSuccess: () => {
      toast.success('图片模型配置已保存')
      queryClient.invalidateQueries({ queryKey: queryKeys.modelConfig.all })
    },
    onError: (err) => toast.error(getApiErrorMessage(err, '保存失败')),
  })

  const clearMutation = useMutation({
    mutationFn: () => api.modelConfig.clear(),
    onSuccess: () => {
      toast.success('已恢复系统默认配置')
      queryClient.invalidateQueries({ queryKey: queryKeys.modelConfig.all })
      setTextModel('')
      setTextEndpoint('')
      setTextApiKey('')
      setTextProxy('')
      setImageProvider('')
      setImageModel('')
      setImageEndpoint('')
      setImageApiKey('')
      setImageProxy('')
    },
    onError: (err) => toast.error(getApiErrorMessage(err, '恢复默认失败')),
  })

  const handleEditText = () => {
    if (textEndpoint && !textModel) {
      toast.error('请填写模型名称')
      return
    }
    textMutation.mutate()
  }

  const handleEditImage = () => {
    const hasAnyImageConfig = !!(imageProvider || imageEndpoint || imageApiKey || imageModel || imageProxy)
    if (hasAnyImageConfig && !(imageProvider && imageEndpoint && imageApiKey && imageModel)) {
      toast.error('图片模型需要同时填写服务商、Endpoint、API Key 和模型名称')
      return
    }
    imageMutation.mutate()
  }

  if (isLoading) return <p className="text-xs text-muted-foreground">加载中...</p>

  return (
    <Card>
      <div className="border-b border-border px-4 py-3 flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-foreground">MCP 模型配置</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            仅用于 MCP 服务端工具中的写作、排版和图片生成，不会改变 Claude Code Agent 使用的模型。
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="text-red-500 hover:text-red-600"
          onClick={() => clearMutation.mutate()}
          disabled={!textHasConfig && !imageHasConfig}
        >
          恢复系统默认
        </Button>
      </div>
      <CardContent className="space-y-4">
        {/* Text Model */}
        <ModelSection
          title="服务端写作模型"
          description={
            textHasConfig
              ? '已配置 MCP 写作自定义模型'
              : 'MCP 写作工具使用系统默认模型'
          }
        >
          <MaskedInput
            label="Endpoint"
            value={textEndpoint}
            onChange={setTextEndpoint}
            placeholder="https://api.openai.com/v1"
            hasValue={!!config?.text?.endpoint}
          />
          <MaskedInput
            label="API Key"
            value={textApiKey}
            onChange={setTextApiKey}
            placeholder="sk-..."
            hasValue={!!config?.text?.api_key}
          />
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Label className="text-xs">模型名称</Label>
              {config?.text?.model && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
            </div>
            <Input
              value={textModel}
              onChange={(e) => setTextModel(e.target.value)}
              placeholder="gpt-4o / glm-5.1"
              className="text-xs"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">代理服务器（可选）</Label>
            <Input
              value={textProxy}
              onChange={(e) => setTextProxy(e.target.value)}
              placeholder="http://proxy:port"
              className="text-xs"
            />
          </div>
          <div className="flex items-center gap-2 pt-1">
            <Button
              size="sm"
              onClick={handleEditText}
              disabled={textMutation.isPending}
            >
              {textMutation.isPending ? '保存中...' : '保存'}
            </Button>
          </div>
        </ModelSection>

        {/* Image Model */}
        <ModelSection
          title="服务端图片模型"
          description={
            imageHasConfig
              ? '已配置 MCP 图片自定义模型'
              : 'MCP 图片工具使用系统默认模型'
          }
        >
          <p className="px-3 text-xs text-muted-foreground">
            图片自定义配置需要完整填写服务商、Endpoint、API Key 和模型名称。OpenAI 选项只适用于 OpenAI Images API 兼容接口。
          </p>
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Label className="text-xs">图片服务商</Label>
              {config?.image?.provider && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
            </div>
            <select
              value={imageProvider}
              onChange={(e) => setImageProvider(e.target.value)}
              className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-xs"
            >
              <option value="">选择服务商</option>
              {IMAGE_PROVIDERS.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
            {imageProvider && (
              <p className="text-xs text-muted-foreground">{imageProviderNotes[imageProvider]}</p>
            )}
          </div>
          <MaskedInput
            label="Endpoint"
            value={imageEndpoint}
            onChange={setImageEndpoint}
            placeholder="https://..."
            hasValue={!!config?.image?.endpoint}
          />
          <MaskedInput
            label="API Key"
            value={imageApiKey}
            onChange={setImageApiKey}
            placeholder="..."
            hasValue={!!config?.image?.api_key}
          />
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Label className="text-xs">模型名称</Label>
              {config?.image?.model && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500" />}
            </div>
            <Input
              value={imageModel}
              onChange={(e) => setImageModel(e.target.value)}
              placeholder="dall-e-3"
              className="text-xs"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">代理服务器（可选）</Label>
            <Input
              value={imageProxy}
              onChange={(e) => setImageProxy(e.target.value)}
              placeholder="http://proxy:port"
              className="text-xs"
            />
          </div>
          <div className="flex items-center gap-2 pt-1">
            <Button
              size="sm"
              onClick={handleEditImage}
              disabled={imageMutation.isPending}
            >
              {imageMutation.isPending ? '保存中...' : '保存'}
            </Button>
          </div>
        </ModelSection>
      </CardContent>
    </Card>
  )
}
