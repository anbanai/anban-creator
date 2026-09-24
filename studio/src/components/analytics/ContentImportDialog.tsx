import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronsUpDown, FileSpreadsheet, Loader2, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { contentAnalyticsApi, contentTypeLabel, targetKey } from '@/lib/content-analytics'
import { shanghaiDay } from '@/lib/analytics-period'
import { uploadToOSS } from '@/lib/direct-upload'
import type { Project } from '@/types'
import type { AnalyticsCandidate, AnalyticsPreview, AnalyticsTarget } from '@/types/content-analytics'

const statusLabels: Record<string, string> = { pending: '待开始', running: '进行中', completed: '已完成', failed: '失败', cancelled: '已取消', drafted: '草稿', published: '已发布', awaiting_manual_publish: '待发布', recorded: '已有数据', drafting: '草稿处理中', publishing: '发布中', ambiguous: '待确认', needs_selection: '待选择', publish_failed: '发布失败', unsupported: '需手动发布' }

function TargetPicker({ projectId, rowNumber, target, label, disabled, onChange }: { projectId: string; rowNumber: number; target?: AnalyticsTarget; label: string; disabled: boolean; onChange: (candidate: AnalyticsCandidate) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const query = useQuery({ queryKey: ['content-analytics-candidates', projectId, search, page], queryFn: () => contentAnalyticsApi.candidates(projectId, { search, offset: page * 25, limit: 25 }), enabled: open })
  return <Popover open={open} onOpenChange={setOpen}>
    <PopoverTrigger render={<Button variant="outline" size="sm" className="max-w-64 justify-between" disabled={disabled} aria-label={`第 ${rowNumber} 行选择对应内容`} />}><span className="truncate">{label}</span><ChevronsUpDown className="shrink-0" /></PopoverTrigger>
    <PopoverContent align="end" className="w-[min(26rem,calc(100vw-2rem))]">
      <div className="flex items-center gap-2"><Search className="size-4 text-muted-foreground" /><Input autoFocus aria-label="搜索当前账号内容" placeholder="搜索当前账号内容" value={search} onChange={(event) => { setSearch(event.target.value); setPage(0) }} /></div>
      <div className="max-h-64 space-y-1 overflow-y-auto" aria-label="可匹配内容">
        {query.isPending ? <p role="status" className="p-3 text-muted-foreground">读取内容…</p> : query.isError ? <div role="alert" className="p-3"><p>{query.error.message}</p><Button variant="ghost" onClick={() => void query.refetch()}>重试</Button></div> : !query.data.items.length ? <p className="p-3 text-muted-foreground">没有找到内容，试试其他关键词</p> : query.data.items.map((candidate) => <button key={targetKey(candidate.target)} type="button" className="flex w-full gap-2 rounded-md p-2 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring" onClick={() => { onChange(candidate); setOpen(false) }}>
          <Check className={`mt-1 size-4 shrink-0 ${target && targetKey(target) === targetKey(candidate.target) ? 'text-primary' : 'invisible'}`} /><span className="min-w-0"><span className="block break-words font-medium">{candidate.title || '未命名内容'}</span><span className="mt-1 block text-xs text-muted-foreground">{contentTypeLabel(candidate.content_type)} · {statusLabels[candidate.status] ?? '已有内容'}{candidate.date ? ` · ${candidate.date.slice(0, 10)}` : ''} · {candidate.target.id.slice(-6)}</span></span>
        </button>)}
      </div>
      <div className="flex items-center justify-between border-t pt-2 text-xs"><span>{query.data?.total ?? 0} 篇内容 · 所有状态</span><div className="flex gap-1"><Button size="sm" variant="ghost" disabled={page === 0 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><Button size="sm" variant="ghost" disabled={!query.data || (page + 1) * 25 >= query.data.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div></div>
    </PopoverContent>
  </Popover>
}

export async function invalidateContentAnalytics(client: ReturnType<typeof useQueryClient>, projectId: string) {
  await client.invalidateQueries({ predicate: ({ queryKey }) => queryKey[1] === projectId && /^(content-analytics|wechat-import|seednote-import)/.test(String(queryKey[0])) || queryKey[0] === 'task' || queryKey[0] === 'tasks' })
}

export default function ContentImportDialog({ project, onClose, onImported }: { project: Project; onClose: () => void; onImported?: (result: { count: number; date: string }) => void }) {
  const client = useQueryClient()
  const [file, setFile] = useState<File>()
  const [uploadId, setUploadId] = useState('')
  const [preview, setPreview] = useState<AnalyticsPreview>()
  const [targets, setTargets] = useState<Record<number, AnalyticsTarget>>({})
  const [labels, setLabels] = useState<Record<number, string>>({})
  const [selected, setSelected] = useState<number[]>([])
  const [asOf, setAsOf] = useState(`${shanghaiDay(new Date())}T23:59`)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [type, setType] = useState('all')
  const [page, setPage] = useState(0)
  const previewMutation = useMutation({
    mutationFn: async (nextFile: File) => {
      const upload = await uploadToOSS({ purpose: project.platform === 'article' ? 'wechat_analytics_import' : 'seednote_analytics_import', file: nextFile })
      const result = await contentAnalyticsApi.preview(project, upload.uploadSessionId)
      return { result, id: upload.uploadSessionId }
    },
    onSuccess: ({ result, id }) => {
      setPreview(result); setUploadId(id)
      const matches = result.rows.filter((row) => row.target && row.match_status === 'matched' && !row.parse_error)
      setTargets(Object.fromEntries(matches.map((row) => [row.source_row, row.target!])))
      setLabels(Object.fromEntries(matches.map((row) => [row.source_row, row.target_title ?? row.title])))
      setSelected(matches.map((row) => row.source_row))
    },
  })
  const importMutation = useMutation({
    mutationFn: () => contentAnalyticsApi.import(project, { upload_id: uploadId, selections: selected.map((source_row) => ({ source_row, target: targets[source_row] })), data_as_of_at: new Date(`${asOf}:00+08:00`).toISOString(), timezone: 'Asia/Shanghai', client_file_modified_at: file ? new Date(file.lastModified).toISOString() : undefined }),
    onSuccess: async (result) => { await invalidateContentAnalytics(client, project.id); onImported?.(result); onClose() },
  })
  const busy = previewMutation.isPending || importMutation.isPending
  function selectFile(nextFile?: File) {
    if (!nextFile) return
    setError(''); setPreview(undefined); setTargets({}); setLabels({}); setSelected([]); setSearch(''); setType('all'); setPage(0); setUploadId(''); importMutation.reset(); previewMutation.reset()
    const validExtension = project.platform === 'article' ? /\.xlsx?$/i : /\.xlsx$/i
    if (!validExtension.test(nextFile.name)) { setFile(undefined); setError(project.platform === 'article' ? '请选择 .xls 或 .xlsx 文件' : '请选择 .xlsx 文件'); return }
    if (nextFile.size > 20 * 1024 * 1024) { setFile(undefined); setError('文件不能超过 20 MB'); return }
    setFile(nextFile); previewMutation.mutate(nextFile)
  }
  const rows = (preview?.rows ?? []).filter((row) => (type === 'all' || row.content_type === type) && row.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  const types = [...new Set((preview?.rows ?? []).map((row) => row.content_type))]
  const readyRows = rows.filter((row) => targets[row.source_row] && !row.parse_error && row.match_status !== 'invalid')
  const keys = selected.map((row) => targetKey(targets[row]))
  const duplicates = new Set(keys.filter((key, index) => keys.indexOf(key) !== index))
  const currentPage = Math.min(page, Math.max(0, Math.ceil(rows.length / 25) - 1))
  const validTime = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(asOf) && Number.isFinite(Date.parse(`${asOf}:00+08:00`))
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}><DialogContent className="flex max-h-[92dvh] flex-col gap-0 overflow-hidden p-0 max-sm:top-0 max-sm:left-0 max-sm:h-dvh max-sm:max-h-dvh max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none sm:max-w-5xl" closeButtonDisabled={busy}>
    <DialogHeader className="shrink-0 border-b px-6 py-5"><DialogTitle>导入数据</DialogTitle><DialogDescription>{project.name} · {project.platform === 'article' ? '公众号' : '种草笔记'} · {preview ? '检查并导入' : '选择文件'}</DialogDescription></DialogHeader>
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-6">
      <label className={`flex cursor-pointer items-center gap-4 rounded-lg border border-dashed p-5 hover:border-primary focus-within:ring-2 focus-within:ring-ring ${busy ? 'pointer-events-none opacity-60' : ''}`}><FileSpreadsheet className="size-7 shrink-0 text-primary" /><span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{file?.name ?? '选择官方导出表格'}</span><span className="mt-1 block text-xs text-muted-foreground">{file ? '点击重新选择' : `${project.platform === 'article' ? '.xls / .xlsx' : '.xlsx'} · 最大 20 MB`}</span></span><input aria-label="选择导入文件" className="sr-only" type="file" accept={project.platform === 'article' ? '.xls,.xlsx' : '.xlsx'} disabled={busy} onChange={(event) => { selectFile(event.target.files?.[0]); event.currentTarget.value = '' }} /></label>
      {previewMutation.isPending && <p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />正在解析并匹配当前账号内容…</p>}
      {previewMutation.isError && <div role="alert" className="text-sm text-destructive">{previewMutation.error.message}<Button variant="ghost" disabled={!file || busy} onClick={() => file && previewMutation.mutate(file)}>重新解析</Button></div>}
      {preview && <>
        <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-sm">共 <strong>{preview.total_rows}</strong> 条 · 已选 <strong className="text-primary">{selected.length}</strong> 条 · 跳过 {preview.total_rows - selected.length} 条</p><label className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">数据截至时间（北京时间）<Input className="w-auto" type="datetime-local" aria-label="数据截至时间" value={asOf} disabled={busy} onChange={(event) => setAsOf(event.target.value)} /></label></div>
        <p className="text-xs leading-relaxed text-muted-foreground">未匹配的内容可手动选择当前账号内任意状态的内容。未选择的行直接跳过，关联不会改变发布状态。</p>
        <div className="flex flex-wrap gap-2"><Input className="min-w-40 flex-1" aria-label="搜索导入内容" placeholder="搜索表格内容" value={search} onChange={(event) => { setSearch(event.target.value); setPage(0) }} /><select aria-label="导入内容类型" className="h-9 rounded-md border bg-background px-3 text-sm" value={type} onChange={(event) => { setType(event.target.value); setPage(0) }}><option value="all">全部类型</option>{types.map((value) => <option key={value} value={value}>{contentTypeLabel(value)}</option>)}</select></div>
        <div className="overflow-x-auto rounded-lg border"><Table className="min-w-[640px]"><TableHeader><TableRow><TableHead><input type="checkbox" aria-label="选择当前筛选的可导入内容" disabled={busy || !readyRows.length} checked={readyRows.length > 0 && readyRows.every((row) => selected.includes(row.source_row))} onChange={(event) => setSelected(event.target.checked ? [...new Set([...selected, ...readyRows.map((row) => row.source_row)])] : selected.filter((id) => !readyRows.some((row) => row.source_row === id)))} /></TableHead><TableHead>内容</TableHead><TableHead>类型</TableHead><TableHead className="text-right">{project.platform === 'article' ? '阅读人数' : '观看量'}</TableHead><TableHead>关联内容</TableHead></TableRow></TableHeader><TableBody>{rows.slice(currentPage * 25, currentPage * 25 + 25).map((row) => <TableRow key={row.source_row}><TableCell><input type="checkbox" aria-label={`选择第 ${row.source_row} 行`} checked={selected.includes(row.source_row)} disabled={busy || !targets[row.source_row] || Boolean(row.parse_error) || row.match_status === 'invalid'} onChange={(event) => setSelected(event.target.checked ? [...selected, row.source_row] : selected.filter((id) => id !== row.source_row))} /></TableCell><TableCell className="max-w-80"><p className="truncate font-medium" title={row.title}>{row.title || '未提供标题'}</p><p className="mt-1 text-xs text-muted-foreground">第 {row.source_row} 行{(row.published_date || row.first_published_at) ? ` · ${(row.published_date || row.first_published_at)!.slice(0, 10)}` : ''}</p></TableCell><TableCell className="whitespace-nowrap text-xs text-muted-foreground">{contentTypeLabel(row.content_type)}</TableCell><TableCell className="text-right tabular-nums">{(project.platform === 'article' ? row.read_users : row.view_count)?.toLocaleString('zh-CN') ?? '—'}</TableCell><TableCell>{row.parse_error || row.match_status === 'invalid' ? <p className="text-xs text-destructive">{row.parse_error || '格式错误，请修正文件'}</p> : <div className="space-y-1"><TargetPicker projectId={project.id} rowNumber={row.source_row} target={targets[row.source_row]} label={labels[row.source_row] ?? '选择对应内容'} disabled={busy} onChange={(candidate) => { setTargets({ ...targets, [row.source_row]: candidate.target }); setLabels({ ...labels, [row.source_row]: candidate.title }); setSelected([...new Set([...selected, row.source_row])]) }} /><p className={`text-xs ${selected.includes(row.source_row) && duplicates.has(targetKey(targets[row.source_row])) ? 'text-destructive' : 'text-muted-foreground'}`}>{selected.includes(row.source_row) ? duplicates.has(targetKey(targets[row.source_row])) ? '同一内容重复关联，请修改或取消选择' : '已选择，将导入' : row.match_status === 'needs_review' ? '匹配不唯一，未选择则跳过' : '未选择，将跳过'}</p></div>}</TableCell></TableRow>)}</TableBody></Table>{!rows.length && <p className="p-6 text-center text-sm text-muted-foreground">没有符合筛选的内容</p>}</div>
        <div className="flex items-center justify-between text-xs text-muted-foreground"><span>筛选结果 {rows.length} 条</span><div className="flex items-center gap-2"><Button variant="ghost" size="sm" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>上一页</Button>{currentPage + 1} / {Math.max(1, Math.ceil(rows.length / 25))}<Button variant="ghost" size="sm" disabled={(currentPage + 1) * 25 >= rows.length} onClick={() => setPage(currentPage + 1)}>下一页</Button></div></div>
      </>}
      {(error || importMutation.error) && <p role="alert" className="text-sm text-destructive">{error || importMutation.error?.message}</p>}
      {duplicates.size > 0 && <p role="alert" className="text-sm text-destructive">同一内容只能导入一行数据，请处理重复关联。</p>}
    </div>
    <DialogFooter className="mx-0 mb-0 shrink-0 rounded-none border-t bg-card px-6 py-4"><Button variant="outline" disabled={busy} onClick={onClose}>取消</Button><Button disabled={busy || !preview || !selected.length || !validTime || duplicates.size > 0} onClick={() => importMutation.mutate()}>{importMutation.isPending ? '正在导入…' : `确认导入${selected.length ? ` ${selected.length} 条` : ''}`}</Button></DialogFooter>
  </DialogContent></Dialog>
}
