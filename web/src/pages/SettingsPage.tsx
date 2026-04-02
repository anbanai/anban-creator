import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useAuth } from '@/contexts/AuthContext'
import { api, type ConfigScope, type UserConfig, type UpdateConfigRequest } from '@/lib/api'
import Button from '@/components/ui/Button'
import { Card, CardBody } from '@/components/ui/Card'
import { Input, Textarea } from '@/components/ui/Input'

const configScopes: { value: ConfigScope; label: string; description: string }[] = [
  { value: 'article', label: 'Article (WeChat)', description: 'WeChat public account article settings' },
  { value: 'xls', label: 'XLS (Xiaolvshu)', description: 'Xiaolvshu image post settings' },
  { value: 'rednote', label: 'RedNote (Xiaohongshu)', description: 'Xiaohongshu post settings' },
]

interface ConfigFormData {
  name: string
  keywords: string
  positioning: string
  style: string
  theme: string
  author: string
  wechat_app_id: string
  wechat_secret: string
  image_api_config: string // JSON string
}

function emptyConfigForm(): ConfigFormData {
  return {
    name: '',
    keywords: '',
    positioning: '',
    style: '',
    theme: '',
    author: '',
    wechat_app_id: '',
    wechat_secret: '',
    image_api_config: '',
  }
}

function configToForm(config: UserConfig): ConfigFormData {
  return {
    name: config.name || '',
    keywords: config.keywords || '',
    positioning: config.positioning || '',
    style: config.style || '',
    theme: config.theme || '',
    author: config.author || '',
    wechat_app_id: config.wechat_app_id || '',
    wechat_secret: config.wechat_secret || '',
    image_api_config: config.image_api_config
      ? JSON.stringify(config.image_api_config, null, 2)
      : '',
  }
}

function formToConfig(form: ConfigFormData): UpdateConfigRequest {
  let imageApiConfig: UpdateConfigRequest['image_api_config']
  if (form.image_api_config.trim()) {
    try {
      imageApiConfig = JSON.parse(form.image_api_config)
    } catch {
      throw new Error('Invalid JSON in Image API Config')
    }
  }
  return {
    name: form.name || undefined,
    keywords: form.keywords || undefined,
    positioning: form.positioning || undefined,
    style: form.style || undefined,
    theme: form.theme || undefined,
    author: form.author || undefined,
    wechat_app_id: form.wechat_app_id || undefined,
    wechat_secret: form.wechat_secret || undefined,
    image_api_config: imageApiConfig || undefined,
  }
}

export default function SettingsPage() {
  const { user } = useAuth()
  const queryClient = useQueryClient()

  const [activeScope, setActiveScope] = useState<ConfigScope>('article')
  const [form, setForm] = useState<ConfigFormData>(emptyConfigForm())
  const [formError, setFormError] = useState('')
  const [saveSuccess, setSaveSuccess] = useState(false)

  const { data: configs, isLoading: configsLoading } = useQuery({
    queryKey: ['configs'],
    queryFn: () => api.configs.list(),
  })

  function handleScopeChange(scope: ConfigScope) {
    setActiveScope(scope)
    setFormError('')
    setSaveSuccess(false)
    const configData = configs?.find((c) => c.scope === scope)
    if (configData) {
      setForm(configToForm(configData))
    } else {
      setForm(emptyConfigForm())
    }
  }

  // Initialize form on first load
  useEffect(() => {
    if (!configs) return
    const configData = configs.find((c) => c.scope === activeScope)
    if (configData) {
      setForm(configToForm(configData))
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [configs])

  const updateMutation = useMutation({
    mutationFn: (data: UpdateConfigRequest) => api.configs.update(activeScope, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      queryClient.invalidateQueries({ queryKey: ['config', activeScope] })
      setFormError('')
      setSaveSuccess(true)
      setTimeout(() => setSaveSuccess(false), 3000)
    },
    onError: (err) => {
      setFormError(err instanceof Error ? err.message : 'Failed to save configuration.')
    },
  })

  function handleSave() {
    setFormError('')
    setSaveSuccess(false)
    try {
      const data = formToConfig(form)
      updateMutation.mutate(data)
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Invalid form data.')
    }
  }

  function maskSecret(value: string): string {
    if (!value) return ''
    if (value.length <= 8) return '****'
    return value.slice(0, 4) + '****' + value.slice(-4)
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-100">Settings</h1>
        <p className="mt-1 text-sm text-gray-400">Manage your account and content preferences.</p>
      </div>

      {/* Profile Section */}
      <Card>
        <div className="border-b border-gray-700 px-4 py-3">
          <h2 className="text-base font-semibold text-gray-100">Profile</h2>
        </div>
        <CardBody className="space-y-3">
          <div>
            <p className="text-xs text-gray-500">Email</p>
            <p className="text-sm text-gray-200">{user?.email || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">Nickname</p>
            <p className="text-sm text-gray-200">{user?.nickname || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">Member Since</p>
            <p className="text-sm text-gray-200">
              {user?.created_at
                ? new Date(user.created_at).toLocaleDateString('en-US', {
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric',
                  })
                : '--'}
            </p>
          </div>
        </CardBody>
      </Card>

      {/* Content Configs Section */}
      <Card>
        <div className="border-b border-gray-700 px-4 py-3">
          <h2 className="text-base font-semibold text-gray-100">Content Configurations</h2>
        </div>

        {/* Scope tabs */}
        <div className="flex border-b border-gray-700">
          {configScopes.map((scope) => (
            <button
              key={scope.value}
              onClick={() => handleScopeChange(scope.value)}
              className={`px-4 py-2.5 text-sm font-medium transition-colors border-b-2 ${
                activeScope === scope.value
                  ? 'border-blue-500 text-blue-400'
                  : 'border-transparent text-gray-400 hover:text-gray-200'
              }`}
            >
              {scope.label}
            </button>
          ))}
        </div>

        <CardBody>
          {configsLoading ? (
            <div className="flex items-center justify-center py-8">
              <svg className="h-6 w-6 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
            </div>
          ) : (
            <div className="space-y-4">
              <p className="text-xs text-gray-500">
                {configScopes.find((s) => s.value === activeScope)?.description}
              </p>

              <Input
                label="Account Name"
                placeholder="e.g. My Tech Blog"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />

              <Textarea
                label="Keywords"
                placeholder="e.g. technology, AI, software engineering"
                hint="Comma-separated keywords for content generation"
                value={form.keywords}
                onChange={(e) => setForm({ ...form, keywords: e.target.value })}
              />

              <Textarea
                label="Positioning"
                placeholder="e.g. A tech blog focused on practical AI tutorials for developers"
                value={form.positioning}
                onChange={(e) => setForm({ ...form, positioning: e.target.value })}
              />

              <Input
                label="Writing Style"
                placeholder="e.g. casual-science, dan-koe"
                hint="Built-in styles: casual-science, dan-koe, cultural-depth"
                value={form.style}
                onChange={(e) => setForm({ ...form, style: e.target.value })}
              />

              <Input
                label="Theme"
                placeholder="e.g. autumn-warm, spring-fresh"
                hint="Built-in themes: autumn-warm, spring-fresh, ocean-calm"
                value={form.theme}
                onChange={(e) => setForm({ ...form, theme: e.target.value })}
              />

              <Input
                label="Author Name"
                placeholder="e.g. John Doe"
                value={form.author}
                onChange={(e) => setForm({ ...form, author: e.target.value })}
              />

              <Input
                label="WeChat App ID"
                placeholder="wx..."
                value={form.wechat_app_id}
                onChange={(e) => setForm({ ...form, wechat_app_id: e.target.value })}
              />

              <Input
                label="WeChat App Secret"
                type="password"
                placeholder="Enter new secret to update"
                hint={(() => {
                  const cfg = configs?.find((c) => c.scope === activeScope)
                  return cfg?.wechat_secret ? `Current: ${maskSecret(cfg.wechat_secret)}` : 'Not configured'
                })()}
                value={form.wechat_secret}
                onChange={(e) => setForm({ ...form, wechat_secret: e.target.value })}
              />

              <div>
                <label className="mb-1.5 block text-sm font-medium text-gray-300">
                  Image API Config (JSON)
                </label>
                <textarea
                  className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 font-mono text-xs text-gray-100 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                  rows={6}
                  placeholder='{"provider": "gemini", "model": "gemini-3-pro-image-preview"}'
                  value={form.image_api_config}
                  onChange={(e) => setForm({ ...form, image_api_config: e.target.value })}
                />
                <p className="mt-1 text-xs text-gray-500">
                  Provider options: openai, gemini, openrouter, volcengine
                </p>
              </div>

              {formError && (
                <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
                  {formError}
                </div>
              )}

              {saveSuccess && (
                <div className="rounded-lg bg-green-900/50 px-3 py-2 text-sm text-green-300">
                  Configuration saved successfully.
                </div>
              )}

              <div className="flex justify-end">
                <Button
                  onClick={handleSave}
                  loading={updateMutation.isPending}
                >
                  Save Configuration
                </Button>
              </div>
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
