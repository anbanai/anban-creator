import { useEffect, useState } from 'react'
import { Check, Copy, Link2, LoaderCircle, Share2, X } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { taskSharesApi } from '@/lib/api/task-shares'
import type { TaskShareStatus } from '@/types/task-share'

export function TaskShareDialog({ taskId, canCreate }: { taskId: string; canCreate: boolean }) {
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

  return <>
    <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)} aria-label="分享作品"><Share2 className="size-4" />分享</Button>
    <Dialog open={open} onOpenChange={(value) => { if (!busy) setOpen(value) }}>
      <DialogContent className="rounded-lg sm:max-w-[440px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Link2 className="size-4 text-primary" />分享作品</DialogTitle>
          <DialogDescription>持有链接的人可直接阅读最新交付图文。</DialogDescription>
        </DialogHeader>
        <Button className="absolute right-2 top-2" size="icon-sm" variant="ghost" aria-label="关闭对话框" disabled={busy} onClick={() => setOpen(false)}><X /></Button>
        {loading ? <p role="status" className="flex items-center gap-2 py-5 text-muted-foreground"><LoaderCircle className="size-4 animate-spin" />正在读取分享状态…</p> : share ? <>
          {active ? <div className="space-y-2">
            <div className="flex items-center gap-2"><Input aria-label="分享链接" readOnly value={link} className="min-w-0 text-xs" onFocus={(event) => event.target.select()} /><Button variant="outline" size="icon" aria-label="复制链接" title="复制链接" onClick={() => void copy()} disabled={busy}>{copied ? <Check /> : <Copy />}</Button></div>
            <p className="text-xs text-muted-foreground">{copied ? <span role="status" className="mr-2 text-emerald-700">已复制</span> : null}到期：<time dateTime={share.expires_at}>{new Date(share.expires_at!).toLocaleString('zh-CN', { hour12: false })}</time></p>
          </div> : <p className="rounded-md bg-muted/60 px-3 py-2.5 text-sm">{share.status === 'revoked' ? '分享已关闭' : share.status === 'expired' || share.status === 'active' ? '分享链接已到期' : '尚未创建分享链接'}</p>}
          <fieldset disabled={busy || !canCreate} className="space-y-3">
            <legend className="mb-2 text-xs font-medium text-muted-foreground">有效期</legend>
            <div className="grid grid-cols-3 gap-2">
              {[['1', '1 天'], ['7', '7 天'], ['custom', '自定义']].map(([value, label]) => <label key={value} className={`flex cursor-pointer items-center justify-center gap-2 rounded-md border px-2 py-2 text-sm ${preset === value ? 'border-primary bg-primary/5 text-primary' : 'border-border'}`}><input type="radio" name={`share-days-${taskId}`} value={value} checked={preset === value} onChange={() => { setPreset(value); setError('') }} className="accent-primary" />{label}</label>)}
            </div>
            {preset === 'custom' ? <div className="flex items-center gap-2"><Input type="number" min={1} max={365} step={1} aria-label="有效天数" value={custom} onChange={(event) => setCustom(event.target.value)} className="w-28" /><span className="text-xs text-muted-foreground">天，1–365 个整数天</span></div> : null}
          </fieldset>
          {!canCreate ? <p className="text-xs text-muted-foreground">完整正文交付后可创建或更新链接。</p> : null}
          <div className="flex items-center justify-between gap-2 border-t pt-3">
            {active ? <Button variant="ghost" size="sm" disabled={busy} className="text-destructive" onClick={() => void revoke()}>关闭分享</Button> : <span />}
            <Button size="sm" loading={busy} disabled={!canCreate} onClick={() => void save()}>{active ? '更新期限' : '创建链接'}</Button>
          </div>
        </> : <Button variant="outline" onClick={() => setReload((value) => value + 1)}>重试</Button>}
        {error ? <p role="alert" className="text-xs text-destructive">{error}</p> : null}
      </DialogContent>
    </Dialog>
  </>
}
