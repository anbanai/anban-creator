import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Film, Image, Loader2, RefreshCw, Search, Trash2, Upload } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import type { UserMediaAsset } from '@/lib/api/user-media'
import { uploadToOSS } from '@/lib/direct-upload'

const statusLabels: Record<string, string> = { queued: '排队中', analyzing: '解析中', available: '可用', failed: '解析失败' }

export default function UserMediaLibraryPage() {
  const client = useQueryClient()
  const fileRef = useRef<HTMLInputElement | null>(null)
  const [query, setQuery] = useState('')
  const [mediaType, setMediaType] = useState('')
  const [projectID, setProjectID] = useState('')
  const [status, setStatus] = useState('')
  const [notes, setNotes] = useState<Record<string, string>>({})
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const [operationError, setOperationError] = useState<string | null>(null)
  const [removeTarget, setRemoveTarget] = useState<UserMediaAsset | null>(null)
  const projects = useQuery({ queryKey: ['projects', 'media-library'], queryFn: () => api.projects.list({ status: 'active' }), staleTime: 30_000 })
  const assets = useQuery({
    queryKey: ['user-media', query, mediaType, projectID, status],
    queryFn: () => api.userMedia.list({ q: query || undefined, media_type: mediaType || undefined, project_id: projectID || undefined, status: status || undefined, limit: 50 }),
    refetchInterval: (current) => (current.state.data?.items ?? []).some((item) => item.status === 'queued' || item.status === 'analyzing') ? 5000 : false,
  })
  const update = useMutation({ mutationFn: ({ id, data }: { id: string; data: { project_id?: string | null; usage_notes?: string } }) => api.userMedia.update(id, data), onSuccess: () => { setOperationError(null); void client.invalidateQueries({ queryKey: ['user-media'] }) }, onError: (error) => { setOperationError(error instanceof Error ? error.message : '素材更新失败，请重试。') } })
  const retry = useMutation({ mutationFn: (id: string) => api.userMedia.reanalyze(id), onSuccess: () => { setOperationError(null); void client.invalidateQueries({ queryKey: ['user-media'] }) }, onError: (error) => { setOperationError(error instanceof Error ? error.message : '素材重新解析失败，请重试。') } })
  const remove = useMutation({ mutationFn: (id: string) => api.userMedia.remove(id), onSuccess: () => { setOperationError(null); void client.invalidateQueries({ queryKey: ['user-media'] }) }, onError: (error) => { setOperationError(error instanceof Error ? error.message : '素材移除失败，请重试。') } })

  const onFile = async (file: File) => {
    setUploading(true)
    setUploadError(null)
    try {
      const uploaded = await uploadToOSS({ purpose: 'media_library', file })
      await api.userMedia.create({ upload_session_id: uploaded.uploadSessionId, ...(projectID ? { project_id: projectID } : {}) })
      void client.invalidateQueries({ queryKey: ['user-media'] })
    } catch (error) {
      setUploadError(error instanceof Error ? error.message : '素材上传失败，请重试。')
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return <div className="mx-auto w-full max-w-6xl space-y-6 pb-10">
    <PageHeader title="素材库" description="上传图片或视频，AI 会独立解析内容并为创作提供可复用候选。">
      <input ref={fileRef} type="file" accept="image/*,video/*" className="sr-only" aria-label="上传素材" onChange={(event) => { const file = event.target.files?.[0]; if (file) void onFile(file) }} />
      <Button disabled={uploading} onClick={() => fileRef.current?.click()}>{uploading ? <Loader2 className="animate-spin" /> : <Upload />}上传素材</Button>
    </PageHeader>
    {uploadError && <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">{uploadError}</p>}
    {operationError && <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">{operationError}</p>}

    <div className="grid gap-2 border-b border-border pb-4 sm:grid-cols-2 lg:grid-cols-4">
      <label className="flex items-center gap-2 text-sm"><Search className="size-4 text-muted-foreground" /><Input aria-label="搜索素材" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索描述或使用说明" /></label>
      <select aria-label="素材类型" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={mediaType} onChange={(event) => setMediaType(event.target.value)}><option value="">全部类型</option><option value="image">图片</option><option value="video">视频</option></select>
      <select aria-label="素材所属项目" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">全部项目</option>{(projects.data ?? []).map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
      <select aria-label="素材解析状态" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={status} onChange={(event) => setStatus(event.target.value)}><option value="">全部状态</option>{Object.entries(statusLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
    </div>

    {assets.isPending && <p role="status" className="py-12 text-center text-sm text-muted-foreground">正在加载素材库…</p>}
    {assets.isError && <div role="alert" className="py-12 text-center text-sm text-destructive">素材库加载失败，请稍后重试。</div>}
    {!assets.isPending && !assets.isError && (assets.data?.items.length ?? 0) === 0 && <div className="border border-dashed border-border py-16 text-center text-sm text-muted-foreground">还没有符合条件的素材，先上传一张图片或一段视频。</div>}
    <section aria-label="素材列表" className="grid gap-3 md:grid-cols-2">
      {(assets.data?.items ?? []).map((asset) => <article key={asset.id} className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex min-h-40 items-center justify-center bg-muted/30">
          {asset.preview_url && asset.media_type === 'image' ? <img src={asset.preview_url} alt={asset.file_name} className="h-40 w-full object-cover" /> : asset.preview_url && asset.media_type === 'video' ? <video src={asset.preview_url} controls preload="metadata" aria-label={asset.file_name} className="h-40 w-full object-contain" /> : <div className="flex flex-col items-center gap-2 text-muted-foreground">{asset.media_type === 'video' ? <Film className="size-10" /> : <Image className="size-10" />}<span className="max-w-[18rem] truncate text-xs">{asset.file_name}</span></div>}
        </div>
        <div className="space-y-3 p-4">
          <div className="flex items-start justify-between gap-3"><div className="min-w-0"><h2 className="truncate font-medium">{asset.file_name}</h2><p className="mt-1 text-xs text-muted-foreground">{asset.media_type === 'video' ? '视频' : '图片'} · {asset.project_id ? (projects.data ?? []).find((project) => project.id === asset.project_id)?.name ?? '已标记项目' : '跨项目复用'}</p></div><Badge variant={asset.status === 'failed' ? 'destructive' : 'secondary'}>{statusLabels[asset.status] ?? asset.status}</Badge></div>
          {asset.description ? <p className="line-clamp-3 text-sm leading-6">{asset.description}</p> : <p className="text-sm text-muted-foreground">{asset.status === 'failed' ? asset.error_message ?? '解析失败' : 'AI 正在生成内容描述…'}</p>}
          {asset.recommended_uses?.length ? <p className="text-xs text-muted-foreground">适用：{asset.recommended_uses.join('、')}</p> : null}
          <Input aria-label={`${asset.file_name} 使用说明`} value={notes[asset.id] ?? asset.usage_notes ?? ''} onChange={(event) => setNotes((current) => ({ ...current, [asset.id]: event.target.value }))} onBlur={() => { const value = notes[asset.id]; if (value !== undefined && value !== asset.usage_notes) update.mutate({ id: asset.id, data: { usage_notes: value } }) }} placeholder="补充使用说明（可选）" />
          <select aria-label={`${asset.file_name} 所属项目`} className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={asset.project_id ?? ''} disabled={update.isPending} onChange={(event) => update.mutate({ id: asset.id, data: { project_id: event.target.value || null } })}><option value="">跨项目复用</option>{(projects.data ?? []).map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
          <div className="flex justify-end gap-1"><Button variant="ghost" size="sm" disabled={retry.isPending || asset.status === 'queued' || asset.status === 'analyzing'} onClick={() => retry.mutate(asset.id)}><RefreshCw />重新解析</Button><Button variant="ghost" size="sm" className="text-destructive" disabled={remove.isPending} onClick={() => setRemoveTarget(asset)}><Trash2 />移除</Button></div>
        </div>
      </article>)}
    </section>
    <p className="text-xs text-muted-foreground">共 {assets.data?.total ?? 0} 个素材。移除素材只会停止新的创作调用，历史作品引用仍然保留。</p>
    <AlertDialog open={removeTarget !== null} onOpenChange={(open) => { if (!open) setRemoveTarget(null) }}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>移除这个素材？</AlertDialogTitle><AlertDialogDescription>移除后素材不会再用于新的创作，但历史作品引用仍然保留，存储文件不会永久删除。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={remove.isPending} onClick={() => { if (removeTarget) remove.mutate(removeTarget.id, { onSuccess: () => setRemoveTarget(null) }) }}>确认移除</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
  </div>
}
