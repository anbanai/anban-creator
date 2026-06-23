import { useState, useEffect, useCallback, useRef } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Sparkles, RefreshCw } from 'lucide-react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/Select'
import { Switch } from '@/components/ui/switch'
import { ReferenceImageUpload } from '@/components/channels/ReferenceImageUpload'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import type { Template, TemplateType, TemplateVisibility, CreateTemplateRequest, UpdateTemplateRequest } from '@/types'
import { toast } from 'sonner'

const TYPE_OPTIONS: { value: TemplateType; label: string }[] = [
  { value: 'poster', label: '海报' },
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
]

// 从风格描述截取第一段并限长 20 字，作为默认模板名。前后端语义保持一致。
function deriveTemplateName(style: string): string {
  const firstClause = style.trim().split(/[\n。，,.]/)[0]
  return firstClause.slice(0, 20).trim()
}

// structure / example_content are stored server-side as { text: <markdown> }.
// Backfill the textarea from that shape, tolerating legacy plain-string rows.
function extractScaffoldText(value: unknown): string {
  if (!value) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'object' && value !== null) {
    const text = (value as Record<string, unknown>).text
    if (typeof text === 'string') return text
  }
  return ''
}

interface TemplateCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** When provided, edits this template instead of creating a new one. */
  template?: Template | null
  /** Default type for new templates (e.g. seeded from a task/plan context). */
  defaultType?: TemplateType
}

export function TemplateCreateDialog({
  open,
  onOpenChange,
  template,
  defaultType = 'seednote',
}: TemplateCreateDialogProps) {
  const queryClient = useQueryClient()
  const isEditing = !!template

  const [name, setName] = useState('')
  const [type, setType] = useState<TemplateType>(defaultType)
  const [thumbnailUrl, setThumbnailUrl] = useState('')
  const [stylePrompt, setStylePrompt] = useState('')
  const [visibility, setVisibility] = useState<TemplateVisibility>('public')
  const [analyzing, setAnalyzing] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  // Content scaffold (optional, separate from visual style_prompt). The agent
  // receives these via get_channel_profile(task_id) as template_writing_style /
  // template_structure / template_example. structure/example wrap as
  // { text: <markdown> } on submit (matches the backend JSON column shape).
  const [writingStyle, setWritingStyle] = useState('')
  // 排版样式 (theme) — the Markdown→HTML layout theme. Orthogonal to visual
  // style_prompt and writing_style. Surfaced to the agent as template_theme.
  const [theme, setTheme] = useState('')
  const [structure, setStructure] = useState('')
  const [example, setExample] = useState('')
  const [category, setCategory] = useState('')
  // Tags entered as comma-separated text; split on submit. Editing backfills
  // the existing tags joined by ", ".
  const [tagsText, setTagsText] = useState('')
  // 公众号 (article) 人设 — 统一区块（名称即署名 + 写作风格 + 可选人设头像）：
  //   - authorName：名称 = 发布作者名（get_channel_profile.author，precedence
  //     template > channel）。
  //   - authorStyleIntro：写作风格（自由文本 框架/写作方式/笔迹）= template_writing_style。
  //   - authorAvatarUrl：可选人设头像（不入署名）。三者聚合在 PersonaBlock，
  //     可从人设库一键导入；与公众号频道编辑器 UI 完全一致。
  const [authorName, setAuthorName] = useState('')
  const [authorAvatarUrl, setAuthorAvatarUrl] = useState('')
  const [authorStyleIntro, setAuthorStyleIntro] = useState('')

  // Session epoch: incremented every time the dialog opens. Captured at the
  // start of handleSubmit and compared after the await — if the user closed
  // and reopened while the request was in flight, the stale result's side
  // effects (toast + invalidate + close) are dropped instead of leaking into
  // the new session.
  const sessionEpochRef = useRef(0)
  useEffect(() => {
    if (open) sessionEpochRef.current++
  }, [open])

  // Sync form state when opening. Reset loading flags unconditionally so a
  // previous session's in-flight analyze/submit (closed via overlay/Esc) can't
  // leak into the next open.
  useEffect(() => {
    if (!open) {
      setAnalyzing(false)
      setSubmitting(false)
      return
    }
    setAnalyzing(false)
    setSubmitting(false)
    if (template) {
      setName(template.name)
      setType(template.type)
      setThumbnailUrl(template.thumbnail_url)
      setStylePrompt(template.style_prompt)
      setVisibility(template.visibility === 'private' ? 'private' : 'public')
      setWritingStyle(template.writing_style ?? '')
      setTheme(template.theme ?? '')
      setAuthorName(template.author_name ?? '')
      setAuthorAvatarUrl(template.author_avatar_url ?? '')
      setAuthorStyleIntro(template.author_style_intro ?? '')
      // structure / example are stored as { text: ... }; fall back to raw for
      // legacy rows that may have stored plain strings.
      setStructure(extractScaffoldText(template.structure))
      setExample(extractScaffoldText(template.example_content))
      setCategory(template.category ?? '')
      setTagsText(Array.isArray(template.tags) ? template.tags.join(', ') : '')
    } else {
      setName('')
      setType(defaultType)
      setThumbnailUrl('')
      setStylePrompt('')
      setVisibility('public')
      setWritingStyle('')
      setTheme('')
      setAuthorName('')
      setAuthorAvatarUrl('')
      setAuthorStyleIntro('')
      setStructure('')
      setExample('')
      setCategory('')
      setTagsText('')
    }
  }, [open, template, defaultType])

  // Run analyzeImage and fill style + name when result arrives.
  // force=true (manual "重新识别" button) overwrites existing style;
  // force=false (auto on image upload) only fills empty fields to respect user input.
  // A monotonic request id guards against stale results: if the user uploads a
  // new image (or closes/reopens the dialog) while an older analyze is in
  // flight, the older result is dropped instead of clobbering the newer state.
  const analyzeReqIdRef = useRef(0)
  const analyzeStyle = useCallback(async (imageUrl: string, force = false) => {
    if (!imageUrl) return
    const reqId = ++analyzeReqIdRef.current
    setAnalyzing(true)
    try {
      const res = await api.channels.analyzeImage(imageUrl)
      if (reqId !== analyzeReqIdRef.current) return
      if (!res.style) return
      setStylePrompt((prev) => (force || !prev.trim()) ? res.style : prev)
      setName((prev) => (prev.trim() ? prev : deriveTemplateName(res.style)))
    } catch (err) {
      if (reqId !== analyzeReqIdRef.current) return
      toast.error(getApiErrorMessage(err, '风格识别失败，请手动填写或重试'))
    } finally {
      if (reqId === analyzeReqIdRef.current) {
        setAnalyzing(false)
      }
    }
  }, [])

  // Auto-analyze style whenever a new image is uploaded.
  useEffect(() => {
    if (!open) return
    if (!thumbnailUrl) return
    // Skip if this URL was loaded from an existing template (avoid re-analyzing on edit open).
    if (template && thumbnailUrl === template.thumbnail_url) return
    void analyzeStyle(thumbnailUrl, false)
  }, [thumbnailUrl, open, template, analyzeStyle])

  // Mutations intentionally have no onSuccess/onError — those side effects
  // (toast, query invalidation, dialog close) are owned by handleSubmit so
  // they can be guarded by the session epoch. Without this, a mutation
  // initiated in session N that settles after the user closed and reopened
  // for session N+1 would fire its onSuccess and toast/close the wrong
  // session.
  const createMutation = useMutation({
    mutationFn: (data: CreateTemplateRequest) => api.templates.create(data),
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: UpdateTemplateRequest
    }) => api.templates.update(id, data),
  })

  const handleSubmit = async () => {
    // name 可选；为空时从 style_prompt 兜底生成；两者皆空才阻止。
    const finalName = name.trim() || deriveTemplateName(stylePrompt)
    if (!finalName) {
      toast.error('请上传图片或填写模板名称')
      return
    }
    if (!thumbnailUrl) {
      toast.error('请上传一张图片')
      return
    }
    const epoch = sessionEpochRef.current
    setSubmitting(true)
    try {
      const trimmedTags = tagsText
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)
      // Build payload with the scaffold fields. Each scaffold field is only
      // included when non-empty: the backend Update treats "absent = no change"
      // and "non-empty = set", so omitting empties preserves prior values on
      // edit and avoids clobbering with blanks on create.
      const payload: CreateTemplateRequest = {
        name: finalName,
        type,
        thumbnail_url: thumbnailUrl,
        style_prompt: stylePrompt.trim(),
        visibility,
      }
      // Type-aware payload: each type sends ONLY the fields its form renders.
      // This is the definitive guard for the writer-key bug trap — an article or
      // seednote template must NEVER carry writing_style: style_resolve copies it
      // into Task.WritingStyle, which config_builder treats as a writer resource
      // key, so a free-text value there silently fails to resolve any writer.
      // (Editing a legacy article row that backfilled a stale writing_style into
      // state must NOT re-send it on save, since the field isn't rendered.)
      if (type === 'poster') {
        const writingStyleTrimmed = writingStyle.trim()
        if (writingStyleTrimmed) payload.writing_style = writingStyleTrimmed
        const structureTrimmed = structure.trim()
        if (structureTrimmed) payload.structure = structureTrimmed
        const exampleTrimmed = example.trim()
        if (exampleTrimmed) payload.example_content = exampleTrimmed
        const categoryTrimmed = category.trim()
        if (categoryTrimmed) payload.category = categoryTrimmed
        if (trimmedTags.length > 0) payload.tags = trimmedTags
      }
      if (type === 'article') {
        const themeTrimmed = theme.trim()
        if (themeTrimmed) payload.theme = themeTrimmed
        // 作者 (署名, byline) + 写作风格 (imitation): two independent dimensions.
        const authorNameTrimmed = authorName.trim()
        if (authorNameTrimmed) payload.author_name = authorNameTrimmed
        const authorAvatarTrimmed = authorAvatarUrl.trim()
        if (authorAvatarTrimmed) payload.author_avatar_url = authorAvatarTrimmed
        const authorIntroTrimmed = authorStyleIntro.trim()
        if (authorIntroTrimmed) payload.author_style_intro = authorIntroTrimmed
      }
      if (isEditing && template) {
        await updateMutation.mutateAsync({ id: template.id, data: payload as UpdateTemplateRequest })
      } else {
        await createMutation.mutateAsync(payload)
      }
      // Drop side effects if the user closed and reopened while the request
      // was in flight — the new session has its own intent.
      if (epoch !== sessionEpochRef.current) return
      queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success(isEditing ? '模板已更新' : '模板已创建')
      onOpenChange(false)
    } catch {
      if (epoch !== sessionEpochRef.current) return
      toast.error(isEditing ? '更新模板失败，请重试' : '创建模板失败，请重试')
    } finally {
      if (epoch === sessionEpochRef.current) {
        setSubmitting(false)
      }
    }
  }

  // `submitting` alone drives the busy state. We don't include
  // `createMutation.isPending` / `updateMutation.isPending` because those
  // persist across close→reopen cycles (the dialog stays mounted) and would
  // surface a phantom "保存中" for up to ~30s after the user closed mid-submit.
  // `submitting` is owned by handleSubmit and guarded by the session epoch,
  // so it always reflects the current session's intent.
  const busy = submitting

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEditing ? '编辑模板' : '新建模板'}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Name */}
          <div className="space-y-1.5">
            <Label htmlFor="tpl-name">
              名称
              <span className="ml-1.5 text-xs font-normal text-muted-foreground">（可选，留空将根据识别的风格自动生成）</span>
            </Label>
            <Input
              id="tpl-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：暖系生活感"
              maxLength={100}
            />
          </div>

          {/* Type + Visibility (side by side) */}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label>类别</Label>
              <Select value={type} onValueChange={(v) => setType(v as TemplateType)}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择类别">
                    {(value: string) => TYPE_OPTIONS.find((o) => o.value === value)?.label ?? value}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {TYPE_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value} label={opt.label}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>可见性</Label>
              <div className="flex h-8 items-center gap-2 rounded-lg border border-input px-3">
                <Switch
                  checked={visibility === 'public'}
                  onCheckedChange={(v) => setVisibility(v ? 'public' : 'private')}
                />
                <span className="text-sm text-foreground">
                  {visibility === 'public' ? '公开（所有人可见）' : '私有（仅自己）'}
                </span>
              </div>
            </div>
          </div>

          {/* Image + Style prompt (horizontal) */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label htmlFor="tpl-style">图片与风格</Label>
              <div className="flex items-center gap-2">
                {analyzing && (
                  <span className="flex items-center gap-1 text-xs text-primary">
                    <Sparkles className="h-3 w-3 animate-pulse" />
                    识别中…
                  </span>
                )}
                {thumbnailUrl && !analyzing && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-xs"
                    onClick={() => void analyzeStyle(thumbnailUrl, true)}
                  >
                    <RefreshCw className="h-3 w-3" />
                    重新识别
                  </Button>
                )}
              </div>
            </div>
            <div className="flex items-stretch gap-3">
              <div className="relative shrink-0">
                <ReferenceImageUpload
                  value={thumbnailUrl}
                  onChange={setThumbnailUrl}
                  purpose="reference"
                />
                {analyzing && (
                  <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-background/60 backdrop-blur-[1px]">
                    <Loader2 className="h-5 w-5 animate-spin text-primary" />
                  </div>
                )}
              </div>
              {analyzing ? (
                <div className="flex h-32 flex-1 items-center rounded-md border border-input p-3">
                  <div className="w-full space-y-2">
                    <Skeleton className="h-3.5 w-11/12" />
                    <Skeleton className="h-3.5 w-full" />
                    <Skeleton className="h-3.5 w-9/12" />
                    <Skeleton className="h-3.5 w-10/12" />
                  </div>
                </div>
              ) : (
                <Textarea
                  id="tpl-style"
                  value={stylePrompt}
                  onChange={(e) => setStylePrompt(e.target.value)}
                  placeholder="描述视觉风格（艺术流派、画面氛围、质感…）"
                  maxLength={1024}
                  className="h-32 flex-1 resize-none"
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              上传后系统会自动识别视觉风格，你也可以手动调整。
            </p>
          </div>

          {/* 海报 (poster) — legacy 内容脚手架 (writing style / structure /
              example / category / tags). Poster keeps the original form
              unchanged (see no-silent-feature-removal). */}
          {type === 'poster' && (
            <div className="space-y-3 rounded-lg border border-dashed border-input p-3">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">内容脚手架</Label>
                <span className="text-xs text-muted-foreground">可选，创建任务/计划选此模板时送达 AI</span>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="tpl-writing-style" className="text-xs text-muted-foreground">
                  写作风格 / 调性
                </Label>
                <Textarea
                  id="tpl-writing-style"
                  value={writingStyle}
                  onChange={(e) => setWritingStyle(e.target.value)}
                  placeholder="例如：犀利、接地气、像朋友聊天；多用短句和反问"
                  maxLength={1024}
                  className="resize-none"
                  rows={2}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="tpl-structure" className="text-xs text-muted-foreground">
                  内容结构
                </Label>
                <Textarea
                  id="tpl-structure"
                  value={structure}
                  onChange={(e) => setStructure(e.target.value)}
                  placeholder={'例如：\n1. 开头钩子（一句话点出痛点）\n2. 3 个论点（每个配案例）\n3. 行动号召'}
                  className="resize-none font-mono text-xs"
                  rows={4}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="tpl-example" className="text-xs text-muted-foreground">
                  示例内容
                </Label>
                <Textarea
                  id="tpl-example"
                  value={example}
                  onChange={(e) => setExample(e.target.value)}
                  placeholder="贴一段你认可的成稿片段，AI 会模仿它的语气与节奏"
                  className="resize-none"
                  rows={4}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="tpl-category" className="text-xs text-muted-foreground">
                    分类
                  </Label>
                  <Input
                    id="tpl-category"
                    value={category}
                    onChange={(e) => setCategory(e.target.value)}
                    placeholder="例如：个人成长"
                    maxLength={50}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="tpl-tags" className="text-xs text-muted-foreground">
                    标签<span className="ml-1 font-normal">（逗号分隔）</span>
                  </Label>
                  <Input
                    id="tpl-tags"
                    value={tagsText}
                    onChange={(e) => setTagsText(e.target.value)}
                    placeholder="例如：干货, 方法论"
                    maxLength={200}
                  />
                </div>
              </div>
            </div>
          )}

          {/* 公众号 (article) — 人设（名称即署名 + 写作风格 + 可选人设头像，统一区块）
              + 排版风格（实时预览）。名称落到 author_name（=发布作者名），写作风格落到
              author_style_intro（=template_writing_style）。二者共用 PersonaBlock /
              ThemePicker，与公众号频道编辑器 UI 完全一致。 */}
          {type === 'article' && (
            <>
              <PersonaBlock
                authorName={authorName}
                onAuthorName={setAuthorName}
                authorStyleIntro={authorStyleIntro}
                onAuthorStyleIntro={setAuthorStyleIntro}
                authorAvatarUrl={authorAvatarUrl}
                onAuthorAvatarUrl={setAuthorAvatarUrl}
              />
              <ThemePicker theme={theme} onTheme={setTheme} />
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={busy || analyzing}>
            {busy ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                保存中
              </>
            ) : isEditing ? (
              '保存'
            ) : (
              '创建'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
