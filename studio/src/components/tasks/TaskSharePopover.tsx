import { useEffect, useState } from 'react'
import { Check, Clock3, Copy, Link2, LoaderCircle, Share2, X } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import { Input } from '@/components/ui/input'
import { taskSharesApi } from '@/lib/api/task-shares'
import type { TaskShareStatus } from '@/types/task-share'

export function TaskSharePopover({ taskId, canCreate }: { taskId: string; canCreate: boolean }) {
  const [open, setOpen] = useState(false)
  const [share, setShare] = useState<TaskShareStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [preset, setPreset] = useState('7')
  const [custom, setCustom] = useState('7')
  const [copied, setCopied] = useState(false)
  const [reload, setReload] = useState(0)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true); setShare(null); setError(''); setCopied(false); setPreset('7'); setCustom('7')
    taskSharesApi.get(taskId).then((value) => { if (!cancelled) setShare(value) }).catch(() => { if (!cancelled) setError('读取失败，请重试') }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, taskId, reload])

  const active = share?.status === 'active' && !!share.expires_at && Date.parse(share.expires_at) > Date.now()
  const link = active && share?.share_path ? new URL(share.share_path, window.location.origin).href : ''

  async function save() {
    const days = Number(preset === 'custom' ? custom : preset)
    if (!Number.isInteger(days) || days < 1 || days > 365) { setError('请输入 1–365 之间的整数天数'); return }
    setBusy(true); setError(''); setCopied(false)
    try { setShare(await taskSharesApi.save(taskId, days)) } catch { setError('保存失败，请重试') } finally { setBusy(false) }
  }

  async function revoke() {
    setBusy(true); setError('')
    try { await taskSharesApi.revoke(taskId); setShare({ status: 'revoked' }); setCopied(false) } catch { setError('关闭失败，请重试') } finally { setBusy(false) }
  }

  async function copy() {
    setError('')
    try { await navigator.clipboard.writeText(link); setCopied(true) } catch { setError('复制失败，请手动复制链接') }
  }

  return <Popover open={open} modal={false} onOpenChange={(value) => { if (!busy) setOpen(value) }}>
    <PopoverTrigger render={<Button type="button" variant="outline" size="sm" aria-label="分享作品" />}><Share2 className="size-4" />分享</PopoverTrigger>
    <PopoverContent align="end" sideOffset={8} className="w-[352px] max-w-[calc(100vw-2rem)] max-h-[calc(100dvh-2rem)] gap-3.5 overflow-y-auto rounded-xl p-4 shadow-xl">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <PopoverTitle className="flex items-center gap-2 text-sm font-semibold"><span className="flex size-6 items-center justify-center rounded-md bg-primary/10 text-primary"><Link2 className="size-3.5" aria-hidden="true" /></span>分享作品</PopoverTitle>
            {active ? <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:text-emerald-300"><span className="size-1 rounded-full bg-current" />已开启</span> : null}
          </div>
          <PopoverDescription className="mt-1.5 text-xs leading-5">无需登录，直接阅读最新交付图文。</PopoverDescription>
        </div>
        <Button size="icon-xs" variant="ghost" className="text-muted-foreground" aria-label="关闭分享设置" disabled={busy} onClick={() => setOpen(false)}><X /></Button>
      </div>
      {loading ? <p role="status" className="flex items-center gap-2 py-4 text-xs text-muted-foreground"><LoaderCircle className="size-3.5 animate-spin" />正在读取分享状态…</p> : share ? <>
        {active ? <div className="space-y-2">
          <div className="flex items-center gap-1 rounded-lg border border-border/70 bg-muted/30 p-1">
            <Input aria-label="分享链接" readOnly value={link} className="h-8 min-w-0 border-transparent bg-transparent text-xs shadow-none focus-visible:ring-0 md:text-xs" onFocus={(event) => event.target.select()} />
            <Button variant="ghost" size="icon-sm" className="shrink-0 text-primary" aria-label="复制链接" title="复制链接" onClick={() => void copy()} disabled={busy}>{copied ? <Check /> : <Copy />}</Button>
          </div>
          <p className="flex flex-wrap items-center gap-1 text-[11px] leading-4 text-muted-foreground"><Clock3 className="size-3 shrink-0" aria-hidden="true" />到期：<time dateTime={share.expires_at}>{new Date(share.expires_at!).toLocaleString('zh-CN', { hour12: false })}</time>{copied ? <span role="status" className="ml-1 text-emerald-700 dark:text-emerald-300">已复制</span> : null}</p>
        </div> : <p className="rounded-lg bg-muted/40 px-3 py-2.5 text-xs text-muted-foreground">{share.status === 'revoked' ? '分享已关闭' : share.status === 'expired' || share.status === 'active' ? '分享链接已到期' : '尚未创建分享链接'}</p>}
        <fieldset disabled={busy || !canCreate} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-2 disabled:opacity-50">
          <legend className="sr-only">有效期</legend>
          <span aria-hidden="true" className="mr-1 text-xs text-muted-foreground">有效期</span>
          <div className="flex items-center gap-0.5 rounded-lg bg-muted/60 p-1">
            {[['1', '1 天'], ['7', '7 天'], ['custom', '自定义']].map(([value, label]) => <label key={value} className="cursor-pointer">
              <input type="radio" name={`share-days-${taskId}`} value={value} aria-label={label} checked={preset === value} onChange={() => { setPreset(value); setError('') }} className="peer sr-only" />
              <span className="block whitespace-nowrap rounded-md px-2.5 py-1 text-xs text-muted-foreground peer-checked:bg-background peer-checked:font-medium peer-checked:text-primary peer-checked:shadow-sm peer-focus-visible:ring-2 peer-focus-visible:ring-primary/40">{label}</span>
            </label>)}
          </div>
          {preset === 'custom' ? <div className="flex items-center gap-1"><Input type="number" min={1} max={365} step={1} aria-label="有效天数" value={custom} onChange={(event) => setCustom(event.target.value)} className="h-8 w-14 px-2 text-xs md:text-xs" /><span className="text-xs text-muted-foreground">天</span></div> : null}
        </fieldset>
        {!canCreate ? <p className="text-xs text-muted-foreground">完整正文交付后可创建或更新链接。</p> : null}
      </> : <Button variant="outline" size="sm" onClick={() => setReload((value) => value + 1)}>重试</Button>}
      {error ? <p role="alert" className="rounded-lg bg-destructive/5 px-2.5 py-2 text-xs leading-5 text-destructive">{error}</p> : null}
      {share && !loading ? <div className="flex items-center justify-between gap-2 border-t border-border/60 pt-3">
        {active ? <Button variant="ghost" size="sm" disabled={busy} className="text-xs text-muted-foreground hover:bg-destructive/5 hover:text-destructive" onClick={() => void revoke()}>关闭分享</Button> : <span />}
        <Button size="sm" className="px-3.5" loading={busy} disabled={!canCreate} onClick={() => void save()}>{active ? '更新期限' : '创建链接'}</Button>
      </div> : null}
    </PopoverContent>
  </Popover>
}
