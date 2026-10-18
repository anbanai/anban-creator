import { useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, FileCheck2, FileSpreadsheet, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { uploadToOSS } from '@/lib/direct-upload'
import { wechatAccountAnalyticsImportApi, type WechatAccountAnalyticsPreview } from '@/lib/api/wechat-account-analytics-import'
import { getApiErrorMessage } from '@/lib/http-client'
import type { Project } from '@/types'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'

function currentBeijingDateTimeLocal(now = new Date()) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(now)
  const values = Object.fromEntries(parts.filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}T${values.hour}:${values.minute}`
}

function formatMetric(value: number | null | undefined) {
  return value == null ? '—' : value.toLocaleString('zh-CN')
}

function maxDate(preview?: WechatAccountAnalyticsPreview) {
  return preview?.rows.reduce((latest, row) => row.stat_date > latest ? row.stat_date : latest, '') || ''
}

function isContentAnalyticsWorkbookError(error: unknown) {
  return getApiErrorMessage(error, '').includes('公众号内容分析报表')
}

export default function WechatAccountImportDialog({ project, onClose, onImported, onContentImportRequested }: { project: Project; onClose: () => void; onImported?: (result: { count: number; date: string; dateFrom?: string; dateTo?: string }) => void; onContentImportRequested?: (handoff: { file: File; uploadId: string }) => void }) {
  const client = useQueryClient()
  const [file, setFile] = useState<File>()
  const [uploadId, setUploadId] = useState('')
  const [preview, setPreview] = useState<WechatAccountAnalyticsPreview>()
  const [error, setError] = useState('')
  const [asOf] = useState(() => currentBeijingDateTimeLocal())
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID())
  const fileInputRef = useRef<HTMLInputElement>(null)
  const uploadedSessionId = useRef('')
  useEffect(() => { setIdempotencyKey(crypto.randomUUID()) }, [uploadId, file, asOf])

  const previewMutation = useMutation({
    mutationFn: async ({ nextFile }: { nextFile: File }) => {
      const upload = await uploadToOSS({ purpose: 'wechat_analytics_import', file: nextFile })
      uploadedSessionId.current = upload.uploadSessionId
      setUploadId(upload.uploadSessionId)
      const result = await wechatAccountAnalyticsImportApi.preview(project.id, { upload_id: upload.uploadSessionId, timezone: 'Asia/Shanghai' })
      return { result, id: upload.uploadSessionId }
    },
    onSuccess: ({ result, id }) => { setPreview(result); setUploadId(id) },
    onError: (err, variables) => {
      if (isContentAnalyticsWorkbookError(err) && variables?.nextFile && uploadedSessionId.current && onContentImportRequested) {
        setError('')
        onContentImportRequested({ file: variables.nextFile, uploadId: uploadedSessionId.current })
        return
      }
      setError(getApiErrorMessage(err, '文件解析失败，请重试'))
    },
  })
  const importMutation = useMutation({
    mutationFn: () => wechatAccountAnalyticsImportApi.import(project.id, {
      upload_id: uploadId,
      idempotency_key: idempotencyKey,
      data_as_of_at: new Date(`${asOf}:00+08:00`).toISOString(),
      timezone: 'Asia/Shanghai',
      client_file_modified_at: file ? new Date(file.lastModified).toISOString() : undefined,
    }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['wechat-account-analytics', project.id] })
      await client.invalidateQueries({ queryKey: ['content-analytics', project.id], refetchType: 'none' })
      onImported?.({ count: preview?.rows.length ?? 0, date: maxDate(preview), dateFrom: preview?.date_from, dateTo: preview?.date_to })
      onClose()
    },
    onError: (err) => setError(err instanceof Error ? err.message : '导入失败，请重试'),
  })
  const busy = previewMutation.isPending || importMutation.isPending
  const rows = preview?.rows ?? []
  const validRows = rows.filter((row) => !row.parse_error && row.stat_date)
  const canImport = Boolean(preview && uploadId && validRows.length && !preview.errors?.length && !preview.duplicate_dates?.length)

  function selectFile(nextFile?: File) {
    if (!nextFile) return
    setError(''); setPreview(undefined); setUploadId(''); previewMutation.reset(); importMutation.reset()
    if (!/\.xlsx?$/i.test(nextFile.name)) { setFile(undefined); setError('请选择 .xls 或 .xlsx 文件'); return }
    if (nextFile.size > 20 * 1024 * 1024) { setFile(undefined); setError('文件不能超过 20 MB'); return }
    uploadedSessionId.current = ''
    setFile(nextFile); previewMutation.mutate({ nextFile })
  }

  const filePicker = <input ref={fileInputRef} id="wechat-account-import-file" aria-label="选择账号统计文件" className="sr-only" type="file" accept=".xls,.xlsx" disabled={busy} onChange={(event) => { selectFile(event.target.files?.[0]); event.currentTarget.value = '' }} />
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}><DialogContent className="flex max-h-[92dvh] flex-col gap-0 overflow-hidden p-0 max-sm:top-0 max-sm:left-0 max-sm:h-dvh max-sm:max-h-dvh max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none sm:max-w-5xl" closeButtonDisabled={busy}>
    <DialogHeader className="shrink-0 border-b px-6 py-5"><DialogTitle>导入公众号账号统计</DialogTitle><DialogDescription className="flex items-center gap-2"><ProjectIdentity project={project} compact /> · 官方账号数据 · {preview ? '检查并导入' : '选择文件'}</DialogDescription></DialogHeader>
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-6">
      {!file ? <label htmlFor="wechat-account-import-file" className={`flex min-h-36 cursor-pointer items-center justify-center gap-4 rounded-xl border-2 border-dashed p-8 transition-colors hover:border-primary hover:bg-primary/[0.03] focus-within:ring-2 focus-within:ring-ring ${busy ? 'pointer-events-none opacity-60' : ''}`}><FileSpreadsheet className="size-8 shrink-0 text-primary" /><span className="min-w-0"><span className="block text-base font-medium">选择微信公众平台导出表格</span><span className="mt-1 block text-sm text-muted-foreground">账号每日数据 · .xls / .xlsx · 最大 20 MB</span></span>{filePicker}</label> : <label htmlFor="wechat-account-import-file" className={`flex cursor-pointer items-center gap-3 rounded-lg border bg-muted/20 px-4 py-3 transition-colors hover:border-primary focus-within:ring-2 focus-within:ring-ring ${busy ? 'pointer-events-none opacity-60' : ''}`}><FileCheck2 className="size-5 shrink-0 text-primary" /><span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{file.name}</span>{preview && !previewMutation.isPending ? <span className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground"><CheckCircle2 className="size-3.5 text-emerald-600" />文件已解析 · {preview.rows.length} 个日期</span> : <span className="mt-0.5 block text-xs text-muted-foreground">{previewMutation.isPending ? '正在解析…' : '等待解析'}</span>}</span>{filePicker}</label>}
      {previewMutation.isPending && <p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />正在解析账号统计表格…</p>}
      {(error || previewMutation.isError || importMutation.isError) && <div role="alert" className="text-sm text-destructive">{error || '操作失败，请重试'}<Button variant="ghost" disabled={!file || busy} onClick={() => file && previewMutation.mutate({ nextFile: file })}>重新解析</Button></div>}
      {preview && <>
        <div className="grid gap-3 sm:grid-cols-4"><div className="rounded-lg border px-4 py-3"><p className="text-xs text-muted-foreground">报告类型</p><p className="mt-1 text-sm font-semibold">{preview.report_type || '—'}</p></div><div className="rounded-lg border px-4 py-3"><p className="text-xs text-muted-foreground">日期范围</p><p className="mt-1 text-sm font-semibold tabular-nums">{preview.date_from && preview.date_to ? `${preview.date_from} 至 ${preview.date_to}` : '—'}</p></div><div className="rounded-lg border px-4 py-3"><p className="text-xs text-muted-foreground">有效日期</p><p className="mt-1 text-lg font-semibold tabular-nums">{validRows.length}</p></div><div className="rounded-lg border px-4 py-3"><p className="text-xs text-muted-foreground">来源明细</p><p className="mt-1 text-lg font-semibold tabular-nums">{preview.sources.length ? `${preview.sources.length} 条` : '未提供'}</p></div></div>
        {preview.supported_fields?.length ? <p className="text-xs text-muted-foreground">已识别字段：{preview.supported_fields.join('、')}</p> : null}
        {preview.duplicate_dates?.length ? <p className="text-sm text-destructive">存在重复日期：{preview.duplicate_dates.join('、')}。请修正导出文件后重新导入。</p> : null}
        {preview.duplicate_sources?.length ? <p className="text-sm text-destructive">存在重复来源明细：{preview.duplicate_sources.join('、')}。请修正导出文件后重新导入。</p> : null}
        {preview.errors?.length ? <div className="space-y-1 text-sm text-destructive"><p>文件存在校验错误，无法导入：</p>{preview.errors.slice(0, 8).map((item) => <p key={item}>· {item}</p>)}{preview.errors.length > 8 && <p>· 其余 {preview.errors.length - 8} 项错误未展开</p>}</div> : null}
        {preview.unknown_headers?.length ? <p className="text-xs text-muted-foreground">未识别列将保留在原始记录中：{preview.unknown_headers.join('、')}</p> : null}
        <div className="overflow-x-auto rounded-lg border"><Table className="min-w-[880px]"><TableHeader><TableRow><TableHead>日期</TableHead><TableHead className="text-right">阅读人数</TableHead><TableHead className="text-right">分享人数</TableHead><TableHead className="text-right">跳转阅读原文</TableHead><TableHead className="text-right">点赞人数</TableHead><TableHead className="text-right">新增关注</TableHead><TableHead>状态</TableHead></TableRow></TableHeader><TableBody>{rows.slice(0, 100).map((row) => <TableRow key={`${row.source_row}:${row.stat_date}`}><TableCell className="whitespace-nowrap">{row.stat_date || '—'}</TableCell><TableCell className="text-right tabular-nums">{formatMetric(row.read_users)}</TableCell><TableCell className="text-right tabular-nums">{formatMetric(row.share_users)}</TableCell><TableCell className="text-right tabular-nums">{formatMetric(row.original_read_users)}</TableCell><TableCell className="text-right tabular-nums">{formatMetric(row.like_users)}</TableCell><TableCell className="text-right tabular-nums">{formatMetric(row.follower_gain_count)}</TableCell><TableCell className={row.parse_error ? 'text-destructive' : 'text-muted-foreground'}>{row.parse_error || '可导入'}</TableCell></TableRow>)}</TableBody></Table>{rows.length > 100 && <p className="border-t p-3 text-center text-xs text-muted-foreground">仅展示前 100 个日期，全部有效数据都会导入。</p>}</div>
      </>}
    </div>
    <DialogFooter className="mx-0 mb-0 shrink-0 rounded-none border-t bg-card px-6 py-4"><Button variant="outline" disabled={busy} onClick={onClose}>取消</Button><Button disabled={busy || !canImport} onClick={() => importMutation.mutate()}>{importMutation.isPending ? '正在导入…' : `确认导入${validRows.length ? ` ${validRows.length} 天` : ''}`}</Button></DialogFooter>
  </DialogContent></Dialog>
}
