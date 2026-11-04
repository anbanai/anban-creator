import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import type { TopicSubscription, TopicSubscriptionSettings } from '@/types/topic-radar'

const defaultWeights = { traffic_potential: .25, account_fit: .2, differentiation: .15, timeliness: .1, monetization: .15, production_cost: .08, compliance_risk: .07 }
const platforms: Record<string, string> = { weibo: '微博', douyin: '抖音', zhihu: '知乎', bilibili: 'B站', baidu: '百度', toutiao: '头条' }
const inputClass = 'h-9 w-full rounded-md border bg-background px-2 text-sm'

export function SubscriptionSettings({ subscription, onClose, onSaved, onDeleted = async () => {} }: { subscription: TopicSubscription; onClose: () => void; onSaved: () => Promise<void>; onDeleted?: () => Promise<void> }) {
  const [form, setForm] = useState<TopicSubscriptionSettings>(() => ({ name: subscription.name, enabled: subscription.enabled, timezone: subscription.timezone, schedule: subscription.schedule ?? { hour: 9, minute: 0 }, source_config: { platforms: subscription.source_config?.platforms ?? [] }, weights: subscription.weights ?? defaultWeights, script_policy: { ...subscription.script_policy, enabled: subscription.script_policy?.enabled || subscription.script_policy?.auto_generate || false, top_n: subscription.script_policy?.top_n || 1, platform: subscription.script_policy?.platform || 'douyin', duration_seconds: subscription.script_policy?.duration_seconds || 60, aspect_ratio: subscription.script_policy?.aspect_ratio || '9:16' } }))
  const [deleteOpen, setDeleteOpen] = useState(false)
  const save = useMutation({ mutationFn: () => api.topicRadar.updateSubscription(subscription.id, form), onSuccess: async () => { await onSaved(); toast.success('订阅配置已保存'); onClose() }, onError: () => toast.error('保存失败，请检查时间、权重和脚本设置') })
  const remove = useMutation({ mutationFn: () => api.topicRadar.deleteSubscription(subscription.id), onSuccess: async () => { await onDeleted(); toast.success('订阅已删除'); onClose() }, onError: () => toast.error('删除订阅失败，请稍后重试') })
  const patchPolicy = (patch: Partial<NonNullable<TopicSubscriptionSettings['script_policy']>>) => setForm((current) => ({ ...current, script_policy: { ...current.script_policy, ...patch } }))
  const setPlatform = (platform: string, checked: boolean) => setForm((current) => ({ ...current, source_config: { platforms: checked ? [...(current.source_config?.platforms ?? []), platform] : (current.source_config?.platforms ?? []).filter((value) => value !== platform) } }))

  return <>
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>自动找选题</DialogTitle>
          <DialogDescription>选好要自动完成的事情，其他设置可以保持默认。</DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); save.mutate() }}>
          <div className="rounded-md border p-4">
            <div className="flex items-center justify-between gap-4">
              <span><span className="block font-medium">每天自动找选题</span><span className="mt-1 block text-sm text-muted-foreground">为这个人设持续补充新选题</span></span>
              <Switch aria-label="每天自动找选题" checked={form.enabled} onCheckedChange={(enabled) => setForm((current) => ({ ...current, enabled }))} />
            </div>
            {form.enabled && <label className="mt-4 grid max-w-xs gap-1 text-sm">每天几点运行<input className={inputClass} type="time" required value={`${String(form.schedule!.hour).padStart(2, '0')}:${String(form.schedule!.minute).padStart(2, '0')}`} onChange={(event) => { const [hour, minute] = event.target.value.split(':').map(Number); setForm((current) => ({ ...current, schedule: { hour, minute } })) }} /></label>}
          </div>

          <div className="rounded-md border p-4">
            <div className="flex items-center justify-between gap-4">
              <span><span className="block font-medium">自动生成短视频脚本</span><span className="mt-1 block text-sm text-muted-foreground">为新选题准备一份可编辑的脚本</span></span>
              <Switch aria-label="自动生成短视频脚本" checked={form.script_policy!.enabled} onCheckedChange={(enabled) => patchPolicy({ enabled, auto_generate: false })} />
            </div>
            {form.script_policy!.enabled && <details className="mt-4 border-t pt-3">
              <summary className="cursor-pointer text-sm text-muted-foreground">调整脚本格式</summary>
              <fieldset className="mt-3 grid grid-cols-2 gap-3">
                <legend className="sr-only">脚本设置</legend>
                <label className="grid gap-1 text-sm">每次生成数量<input aria-label="每次生成数量" className={inputClass} type="number" min="1" max="20" required value={form.script_policy!.top_n} onChange={(event) => patchPolicy({ top_n: Number(event.target.value) })} /></label>
                <label className="grid gap-1 text-sm">目标平台<select aria-label="脚本目标平台" className={inputClass} value={form.script_policy!.platform} onChange={(event) => patchPolicy({ platform: event.target.value })}><option value="douyin">抖音</option><option value="channels">视频号</option><option value="bilibili">B站</option><option value="xiaohongshu">小红书</option></select></label>
                <label className="grid gap-1 text-sm">时长（秒）<input aria-label="脚本时长（秒）" className={inputClass} type="number" min="15" max="300" required value={form.script_policy!.duration_seconds} onChange={(event) => patchPolicy({ duration_seconds: Number(event.target.value) })} /></label>
                <label className="grid gap-1 text-sm">画幅<select aria-label="脚本画幅" className={inputClass} value={form.script_policy!.aspect_ratio} onChange={(event) => patchPolicy({ aspect_ratio: event.target.value })}><option>9:16</option><option>16:9</option><option>1:1</option></select></label>
              </fieldset>
            </details>}
          </div>

          <details className="rounded-md border px-4">
            <summary className="cursor-pointer py-3 text-sm font-medium">更多设置</summary>
            <div className="space-y-4 border-t py-4">
              <fieldset className="space-y-2"><legend className="mb-2 text-sm font-medium">趋势来源</legend><div className="flex flex-wrap gap-3">{Object.entries(platforms).map(([id, label]) => <label key={id} className="flex items-center gap-1 text-sm"><input type="checkbox" checked={(form.source_config?.platforms ?? []).includes(id)} onChange={(event) => setPlatform(id, event.target.checked)} />{label}</label>)}</div><p className="text-xs text-muted-foreground">未选择时使用全部支持的趋势来源。</p></fieldset>
              <Button type="button" variant="ghost" className="text-destructive hover:text-destructive" onClick={() => setDeleteOpen(true)} disabled={save.isPending || remove.isPending}>删除自动找题设置</Button>
            </div>
          </details>

          <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={onClose} disabled={remove.isPending}>取消</Button><Button type="submit" disabled={save.isPending || remove.isPending || !Object.values(form.weights!).some((weight) => weight > 0)}>保存配置</Button></div>
        </form>
      </DialogContent>
    </Dialog>
    <AlertDialog open={deleteOpen} onOpenChange={(open) => { if (!remove.isPending) setDeleteOpen(open) }}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>删除这个订阅？</AlertDialogTitle><AlertDialogDescription>删除后不会再启动新的分析，已有运行记录和已入库选题仍会保留。</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel><AlertDialogAction disabled={remove.isPending} onClick={() => remove.mutate()}>{remove.isPending ? '删除中…' : '确认删除'}</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </>
}
