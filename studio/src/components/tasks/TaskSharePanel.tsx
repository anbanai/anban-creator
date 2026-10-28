import { useEffect, useId, useState } from 'react'
import { Check, ChevronDown, Clock3, Copy, Link2, LoaderCircle } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Input } from '@/components/ui/input'
import { taskSharesApi } from '@/lib/api/task-shares'
import type { TaskShareStatus } from '@/types/task-share'

export function TaskSharePanel({ taskId, canCreate }: { taskId: string; canCreate: boolean }) {
  const panelId = useId()
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

  return <div className="mt-4 rounded-xl border border-primary/15 bg-primary/[0.025] p-4 sm:p-5">
    <div className="flex items-start justify-between gap-2">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Link2 className="size-4" aria-hidden="true" /></span>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">分享作品</h3>
            {active ? <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-300"><span className="size-1 rounded-full bg-current" />分享中</span> : null}
          </div>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">通过链接，无需登录即可阅读最新图文。</p>
        </div>
      </div>
      <Button type="button" variant="ghost" size="sm" className="shrink-0 text-muted-foreground" disabled={busy} onClick={() => setOpen((value) => !value)} aria-label={open ? '收起分享设置' : '分享作品'} aria-expanded={open} aria-controls={panelId}>
        {open ? '收起' : '设置分享'}<ChevronDown className={`size-4 ${open ? 'rotate-180' : ''}`} />
      </Button>
    </div>
    {open ? <section id={panelId} aria-label="分享设置" className="mt-5 space-y-5 border-t border-primary/10 pt-5">
      {loading ? <p role="status" className="flex items-center gap-2 py-5 text-muted-foreground"><LoaderCircle className="size-4 animate-spin" />正在读取分享状态…</p> : share ? <>
        <div className="space-y-2.5">
          <p className="text-xs font-medium text-muted-foreground">分享链接</p>
          {active ? <>
            <div className="flex items-center gap-2">
              <Input aria-label="分享链接" readOnly value={link} className="h-10 min-w-0 bg-background text-xs shadow-none" onFocus={(event) => event.target.select()} />
              <Button variant="outline" className="h-10 shrink-0 bg-background" aria-label="复制链接" onClick={() => void copy()} disabled={busy}>{copied ? <Check /> : <Copy />}<span className="hidden sm:inline">复制链接</span></Button>
            </div>
            <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground"><Clock3 className="size-3" aria-hidden="true" />到期：<time dateTime={share.expires_at}>{new Date(share.expires_at!).toLocaleString('zh-CN', { hour12: false })}</time>{copied ? <span role="status" className="ml-1 text-emerald-700 dark:text-emerald-300">已复制</span> : null}</p>
          </> : <p className="rounded-lg border border-dashed border-border bg-background/70 px-3 py-3 text-sm text-muted-foreground">{share.status === 'revoked' ? '分享已关闭' : share.status === 'expired' || share.status === 'active' ? '分享链接已到期' : '尚未创建分享链接'}</p>}
        </div>
        <fieldset disabled={busy || !canCreate} className="min-w-0 space-y-3">
          <legend className="mb-2.5 text-xs font-medium text-muted-foreground">有效期</legend>
          <div className="grid grid-cols-3 gap-2">
            {[['1', '1 天'], ['7', '7 天'], ['custom', '自定义']].map(([value, label]) => <label key={value} className={`flex cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-lg border px-2 py-2.5 text-sm transition-colors ${preset === value ? 'border-primary/60 bg-primary/5 font-medium text-primary' : 'border-border/70 bg-background hover:border-primary/30'}`}><input type="radio" name={`share-days-${taskId}`} value={value} checked={preset === value} onChange={() => { setPreset(value); setError('') }} className="shrink-0 accent-primary" />{label}</label>)}
          </div>
          {preset === 'custom' ? <div className="flex items-center gap-2"><Input type="number" min={1} max={365} step={1} aria-label="有效天数" value={custom} onChange={(event) => setCustom(event.target.value)} className="w-24 bg-background" /><span className="text-xs text-muted-foreground">天，支持 1–365 天</span></div> : null}
        </fieldset>
        {!canCreate ? <p className="text-xs text-muted-foreground">完整正文交付后可创建或更新链接。</p> : null}
        <div className="flex items-center justify-between gap-2 border-t border-primary/10 pt-4">
          {active ? <Button variant="ghost" size="sm" disabled={busy} className="text-destructive" onClick={() => void revoke()}>关闭分享</Button> : <span />}
          <Button size="sm" loading={busy} disabled={!canCreate} onClick={() => void save()}>{active ? '更新期限' : '创建链接'}</Button>
        </div>
      </> : <Button variant="outline" onClick={() => setReload((value) => value + 1)}>重试</Button>}
      {error ? <p role="alert" className="text-xs text-destructive">{error}</p> : null}
    </section> : null}
  </div>
}
