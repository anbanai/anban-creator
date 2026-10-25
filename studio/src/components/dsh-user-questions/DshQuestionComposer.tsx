import { useEffect, useState, type ChangeEvent, type KeyboardEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, CircleHelp, FileText, Loader2, Maximize2, SkipForward } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'

export interface DshQuestionOption {
  label: string
  description?: string
  recommended?: boolean
}

export interface DshQuestion {
  id: string
  question: string
  header?: string
  detail?: string
  options?: DshQuestionOption[]
  multiSelect?: boolean
  multi_select?: boolean
  intent?: {
    kind: 'plan-review'
    approve: string
    callId?: string
  }
}

export interface DshQuestionAnswerItem {
  id: string
  selected: string[]
  custom?: string
}

export interface DshQuestionAnswer {
  answers: DshQuestionAnswerItem[]
}

interface DraftAnswer {
  selected: string[]
  custom: string
  skipped: boolean
}

const MAX_CUSTOM_ANSWER_RUNES = 20000

function runeLength(value: string) {
  return Array.from(value).length
}

function truncateRunes(value: string) {
  return Array.from(value).slice(0, MAX_CUSTOM_ANSWER_RUNES).join('')
}

function parseRecommendedLabel(label: string) {
  const suffix = /\s*(?:\((?:recommended|推荐)\)|（(?:recommended|推荐)）)\s*$/i
  return suffix.test(label)
    ? { label: label.replace(suffix, ''), recommended: true }
    : { label, recommended: false }
}

function optionDisplay(option: DshQuestionOption) {
  const parsed = parseRecommendedLabel(option.label)
  return {
    label: parsed.label,
    recommended: option.recommended === true || parsed.recommended,
  }
}

function mediaAssetID(label: string) {
  const match = /^media_asset:(.+)$/.exec(label.trim())
  return match?.[1]?.trim() || undefined
}

function MediaCandidateOption({ option, selected, disabled, multi, onChoose }: { option: DshQuestionOption; selected: boolean; disabled: boolean; multi: boolean; onChoose: () => void }) {
  const id = mediaAssetID(option.label)
  const media = useQuery({ queryKey: ['user-media', id], queryFn: () => api.userMedia.get(id as string), enabled: Boolean(id), staleTime: 60_000 })
  const item = media.data
  const unavailable = media.isError || item?.status !== undefined && item.status !== 'available'
  return <button type="button" role={multi ? 'checkbox' : 'radio'} aria-checked={selected} aria-label={item?.file_name ?? option.description ?? option.label} disabled={disabled || unavailable} onClick={onChoose} className={`flex min-h-20 w-full items-center gap-3 rounded-md border px-3 py-2 text-left transition-colors disabled:cursor-default disabled:opacity-60 ${selected ? 'border-primary/50 bg-primary/5' : 'border-transparent hover:bg-muted/70'}`}>
    {item?.preview_url && item.media_type === 'image' ? <img src={item.preview_url} alt="" className="size-14 shrink-0 rounded object-cover" /> : item?.preview_url && item.media_type === 'video' ? <video src={item.preview_url} muted preload="metadata" aria-label={item.file_name} className="size-14 shrink-0 rounded object-cover" /> : <span className="grid size-14 shrink-0 place-items-center rounded bg-muted text-xs text-muted-foreground">{item?.media_type === 'video' ? '视频' : '素材'}</span>}
    {multi ? <span aria-hidden="true" className={`flex size-4 shrink-0 items-center justify-center rounded-[4px] border ${selected ? 'border-primary bg-primary text-primary-foreground' : 'border-input'}`}>{selected && <Check className="size-3" />}</span> : null}
    <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{item?.file_name ?? (media.isError ? '素材暂不可用' : '正在加载素材…')}</span><span className="mt-1 line-clamp-3 text-xs leading-5 text-muted-foreground">{unavailable ? '素材已不可用，请选择其他素材或重新上传。' : item?.description ?? '候选素材'}</span>{!unavailable && option.description && <span className="mt-1 block line-clamp-2 text-xs leading-5 text-muted-foreground">{option.description}</span>}</span>
  </button>
}

function isComposing(event: KeyboardEvent<HTMLTextAreaElement>) {
  return event.nativeEvent.isComposing || Reflect.get(event.nativeEvent, 'keyCode') === 229
}

function isAnswered(draft: DraftAnswer) {
  return draft.selected.length > 0 || draft.custom.trim() !== ''
}

export function DshQuestionComposer({
  questions,
  onSubmit,
  submitted = false,
  answers = [],
  disabled = false,
}: {
  questions: DshQuestion[]
  onSubmit: (answer: DshQuestionAnswer) => Promise<void>
  submitted?: boolean
  answers?: DshQuestionAnswerItem[]
  disabled?: boolean
}) {
  const [drafts, setDrafts] = useState<DraftAnswer[]>(() => questions.map((question) => ({
    selected: answers.find((answer) => answer.id === question.id)?.selected
      ?? (() => {
        const recommended = question.options?.find((option) => optionDisplay(option).recommended)
        return recommended ? [recommended.label] : []
      })(),
    custom: answers.find((answer) => answer.id === question.id)?.custom ?? '',
    skipped: answers.some((answer) => answer.id === question.id && answer.selected.length === 0 && !answer.custom),
  })))
  const [index, setIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [minimized, setMinimized] = useState(submitted)
  const [submittedState, setSubmittedState] = useState(submitted)
  const [expandedEditor, setExpandedEditor] = useState(false)

  useEffect(() => {
    if (!submitted || answers.length === 0) return
    setDrafts(questions.map((question) => {
      const answer = answers.find((item) => item.id === question.id)
      return {
        selected: answer?.selected ?? [],
        custom: answer?.custom ?? '',
        skipped: answer !== undefined && answer.selected.length === 0 && !answer.custom,
      }
    }))
    setMinimized(true)
  }, [answers, questions, submitted])

  if (questions.length === 0) return null

  const isSubmitted = submitted || submittedState
  const interactionDisabled = disabled || busy

  const question = questions[index]
  const draft = drafts[index]
  const multi = question.multiSelect ?? question.multi_select ?? false
  const answered = isAnswered(draft)
  const updateDraft = (update: (value: DraftAnswer) => DraftAnswer, nextIndex = index) => {
    setDrafts((items) => items.map((item, itemIndex) => itemIndex === index ? update(item) : item))
    setIndex(nextIndex)
    setError(null)
  }
  const buildAnswer = (values: DraftAnswer[]): DshQuestionAnswer => ({
    answers: questions.map((item, itemIndex) => {
      const value = values[itemIndex]
      if (value.skipped) return { id: item.id, selected: [] }
      const custom = value.custom
      return {
        id: item.id,
        selected: custom !== '' && !(item.multiSelect ?? item.multi_select ?? false) ? [] : value.selected,
        ...(custom === '' ? {} : { custom }),
      }
    }),
  })
  const submit = async (values = drafts) => {
    const missing = values.findIndex((value) => !isAnswered(value) && !value.skipped)
    if (missing >= 0) {
      setIndex(missing)
      setError('请先回答或跳过这道问题。')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await onSubmit(buildAnswer(values))
      setSubmittedState(true)
      setMinimized(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '答案发送失败，请重试。')
    } finally {
      setBusy(false)
    }
  }
  const continueFlow = () => {
    if (!answered) {
      setError('请选择一个选项或填写自定义答案。')
      return
    }
    if (index < questions.length - 1) {
      setIndex(index + 1)
      setError(null)
      return
    }
    void submit()
  }
  const skipQuestion = () => {
    const nextDrafts = drafts.map((value, itemIndex) => itemIndex === index
      ? { selected: [], custom: '', skipped: true }
      : value)
    setDrafts(nextDrafts)
    setError(null)
    if (index < questions.length - 1) {
      setIndex(index + 1)
      return
    }
    void submit(nextDrafts)
  }
  const choose = (label: string) => {
    const next = multi
      ? {
        ...draft,
        selected: draft.selected.includes(label)
          ? draft.selected.filter((item) => item !== label)
          : [...draft.selected, label],
        skipped: false,
      }
      : { selected: [label], custom: '', skipped: false }
    updateDraft(() => next, !multi && index < questions.length - 1 ? index + 1 : index)
  }
  const updateCustom = (event: ChangeEvent<HTMLTextAreaElement>) => {
    const rawCustom = event.target.value
    const custom = truncateRunes(rawCustom)
    updateDraft((value) => ({
      ...value,
      selected: multi ? value.selected : [],
      custom,
      skipped: false,
    }))
    if (runeLength(rawCustom) > MAX_CUSTOM_ANSWER_RUNES) {
      setError(`自定义答案最多 ${MAX_CUSTOM_ANSWER_RUNES} 个字符，超出部分已截断。`)
    }
  }
  const continueFromCustom = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey || isComposing(event)) return
    event.preventDefault()
    continueFlow()
  }

  const planReview = question.intent?.kind === 'plan-review'
  const customLabel = question.header ? `${question.header}补充说明` : '问题补充说明'
  const editor = (
    <Textarea
      value={draft.custom} onChange={updateCustom} onKeyDown={continueFromCustom}
      disabled={interactionDisabled} aria-label={customLabel}
      placeholder={question.options?.length ? '输入自定义答案，支持粘贴长文档' : '输入你的答案，支持粘贴长文档'}
      rows={question.options?.length ? 4 : 8}
      className="min-h-28 max-h-[min(45dvh,520px)] resize-y text-sm leading-6"
    />
  )

  return (
    <section className="mx-auto w-full max-w-[760px] overflow-hidden rounded-lg border border-border bg-card shadow-sm" aria-busy={busy}>
      <header className="flex items-start justify-between gap-3 px-4 pb-3 pt-4 sm:px-5">
        <div className="min-w-0">
          {question.header && <p className="mb-1 text-xs text-muted-foreground">{question.header}</p>}
          <h2 className="text-sm font-semibold leading-6">{question.question}</h2>
          {isSubmitted && <p className="mt-1 text-xs text-emerald-600">已回答</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <span className="mr-1 text-xs tabular-nums text-muted-foreground">{index + 1} / {questions.length}</span>
          <Button
            type="button" variant="ghost" size="icon-sm"
            aria-label={minimized ? '展开问题卡片' : '收起问题卡片'}
            title={minimized ? '展开问题卡片' : '收起问题卡片'}
            aria-expanded={!minimized} disabled={interactionDisabled}
            onClick={() => setMinimized((value) => !value)}
          >
            {minimized ? <ChevronUp /> : <ChevronDown />}
          </Button>
        </div>
      </header>

      {!minimized && isSubmitted && (
        <>
          <div className="border-t border-border px-4 py-3 text-sm leading-6 text-muted-foreground sm:px-5">
            {isAnswered(draft) ? <p>{[...draft.selected, ...(draft.custom ? [draft.custom] : [])].join('、')}</p> : <p>已跳过</p>}
          </div>
          <footer className="flex items-center justify-between gap-2 border-t border-border px-3 py-3 sm:px-4">
            <Button type="button" variant="ghost" size="sm" disabled={index === 0} onClick={() => setIndex((value) => value - 1)}>
              <ChevronLeft />上一题
            </Button>
            <Button type="button" variant="ghost" size="sm" disabled={index === questions.length - 1} onClick={() => setIndex((value) => value + 1)}>
              下一题<ChevronRight />
            </Button>
          </footer>
        </>
      )}

      {!minimized && !isSubmitted && (
        <>
          {disabled && <p className="border-t border-border px-4 py-2 text-xs text-muted-foreground sm:px-5">会话已结束，重新开始后才能继续回答。</p>}
          <div className="max-h-[min(52dvh,420px)] space-y-3 overflow-y-auto px-3 pb-3 sm:px-4">
            {planReview && (
              <div className="rounded-md border border-primary/20 bg-primary/5 p-3">
                <div className="mb-2 flex items-center gap-2 text-xs font-medium text-primary"><FileText className="size-4" />计划审阅</div>
                <div className="max-h-64 overflow-y-auto rounded border border-border/70 bg-background/70 px-3 py-2 text-sm leading-6"><Streamdown>{question.detail ?? ''}</Streamdown></div>
                <p className="mt-2 text-xs text-muted-foreground">请阅读计划后选择是否执行。</p>
              </div>
            )}
            {question.detail && !planReview && <div className="px-2 text-sm leading-6 text-muted-foreground"><Streamdown>{question.detail}</Streamdown></div>}
            {question.options && question.options.length > 0 && (
              <div className="space-y-1" role={multi ? 'group' : 'radiogroup'} aria-label={question.question}>
                {question.options.map((option, optionIndex) => {
                  const selected = draft.selected.includes(option.label)
                  const display = optionDisplay(option)
                  if (mediaAssetID(option.label)) {
                    return <MediaCandidateOption key={`${option.label}-${optionIndex}`} option={option} selected={selected} disabled={interactionDisabled} multi={multi} onChoose={() => choose(option.label)} />
                  }
                  return (
                    <button
                      key={`${option.label}-${optionIndex}`} type="button"
                      role={multi ? 'checkbox' : 'radio'} aria-checked={selected}
                      aria-label={display.label} disabled={interactionDisabled}
                      onClick={() => choose(option.label)}
                      onKeyDown={(event) => {
                        if (event.key !== 'Enter') return
                        event.preventDefault()
                        void submit()
                      }}
                      className={`flex min-h-10 w-full items-start gap-3 rounded-md border px-3 py-2 text-left transition-colors disabled:cursor-default disabled:opacity-60 ${selected ? 'border-primary/50 bg-primary/5' : 'border-transparent hover:bg-muted/70'}`}
                    >
                      {multi
                        ? <span aria-hidden="true" className={`mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-[4px] border ${selected ? 'border-primary bg-primary text-primary-foreground' : 'border-input'}`}>{selected && <Check className="size-3" />}</span>
                        : <span aria-hidden="true" className="grid size-5 shrink-0 place-items-center rounded-[4px] bg-muted text-xs tabular-nums text-muted-foreground">{optionIndex + 1}</span>}
                      <span className="min-w-0 flex-1 text-sm leading-6">
                        <span className="font-medium">{display.label}</span>
                        {display.recommended && <span className="ml-2 rounded-sm bg-primary/10 px-1.5 py-0.5 text-[11px] font-medium text-primary">推荐</span>}
                        {option.description && <span className="ml-2 text-muted-foreground">{option.description}</span>}
                      </span>
                    </button>
                  )
                })}
                {question.options.length > 0 && (
                  <div className={`flex items-start gap-3 rounded-md border px-3 py-2 ${draft.custom ? 'border-primary/50 bg-muted/40' : 'border-transparent'}`}>
                    {multi
                      ? <span aria-hidden="true" className={`mt-0.5 size-4 shrink-0 rounded-[4px] border ${draft.custom ? 'border-primary bg-primary' : 'border-input'}`} />
                      : <span aria-hidden="true" className="grid size-5 shrink-0 place-items-center rounded-[4px] bg-muted text-muted-foreground"><CircleHelp className="size-3" /></span>}
                    <div className="min-w-0 flex-1">
                      {editor}
                      <div className="mt-2 flex items-center justify-between gap-2">
                        <span className="text-xs tabular-nums text-muted-foreground">{runeLength(draft.custom)} / {MAX_CUSTOM_ANSWER_RUNES}</span>
                        <Button type="button" variant="ghost" size="sm" disabled={interactionDisabled} onClick={() => setExpandedEditor(true)}><Maximize2 />展开编辑</Button>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            )}
            {(!question.options || question.options.length === 0) && (
              <div className="min-w-0 flex-1">
                {editor}
                <div className="mt-2 flex items-center justify-between gap-2">
                  <span className="text-xs tabular-nums text-muted-foreground">{runeLength(draft.custom)} / {MAX_CUSTOM_ANSWER_RUNES}</span>
                  <Button type="button" variant="outline" size="sm" disabled={interactionDisabled} onClick={() => setExpandedEditor(true)}><Maximize2 />展开编辑</Button>
                </div>
              </div>
            )}
          </div>

          <footer className="flex flex-wrap items-center gap-2 border-t border-border px-3 py-3 sm:px-4">
            <div className="flex items-center gap-1">
              <Button type="button" variant="ghost" size="icon-sm" aria-label="上一题" title="上一题" disabled={index === 0 || interactionDisabled} onClick={() => { setIndex(index - 1); setError(null) }}><ChevronLeft /></Button>
              <Button type="button" variant="ghost" size="icon-sm" aria-label="下一题" title="下一题" disabled={index === questions.length - 1 || interactionDisabled} onClick={() => { setIndex(index + 1); setError(null) }}><ChevronRight /></Button>
            </div>
            <div className="min-w-0 flex-1 text-xs text-destructive" role="status">{error}</div>
            <div className="ml-auto flex shrink-0 items-center gap-2">
              <Button type="button" variant="outline" size="sm" disabled={interactionDisabled} onClick={skipQuestion}><SkipForward />跳过</Button>
              <Button type="button" size="sm" disabled={interactionDisabled || !answered} onClick={continueFlow}>
                {busy ? <><Loader2 className="animate-spin" />正在提交</> : index === questions.length - 1 ? <><Check />提交回答</> : '下一题'}
              </Button>
            </div>
          </footer>
        </>
      )}

      <Dialog open={expandedEditor} onOpenChange={setExpandedEditor}>
        <DialogContent className="flex max-h-[92dvh] flex-col gap-3 sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{question.header ? `${question.header}：长文本回答` : '长文本回答'}</DialogTitle>
            <DialogDescription>可以直接粘贴整篇文档，内容会保留在当前问题中。</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1">{editor}</div>
          <div className="text-right text-xs tabular-nums text-muted-foreground">{runeLength(draft.custom)} / {MAX_CUSTOM_ANSWER_RUNES} 个字符</div>
          <DialogFooter>
            <Button type="button" onClick={() => setExpandedEditor(false)}>完成编辑</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
