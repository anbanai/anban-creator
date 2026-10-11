import type { Task, TaskFile } from '@/types'
import { FilePreviewGallery } from '@/components/FilePreview'
import { Button } from '@/components/common/button'
import { Eye, FileText } from 'lucide-react'

// Presentation only: never promote a process/retained file to a deliverable.
export function selectResultFiles(files: TaskFile[], executionId?: string) {
  const delivered = files.filter(file => file.state === 'delivered' && file.is_deliverable === true)
  const current = executionId ? delivered.filter(file => file.execution_id === executionId) : delivered
  const candidates = current.length ? current : delivered
  const html = candidates.filter(file => file.mime_type === 'text/html' || /\.html?$/i.test(file.file_name))
  const main = html.length ? html : candidates.filter(file =>
    ['final_markdown', 'final_video', 'content', 'copywriting', 'markdown'].includes(file.delivery_role || file.role)
    || /(?:^|\/)(?:\d+-)?(?:article-final|article|content|final)\.(md|txt|mp4)$/i.test(file.file_name))
  return { main, other: delivered.filter(file => !main.some(item => item.id === file.id)), historical: !!executionId && main.some(file => !!file.execution_id && file.execution_id !== executionId) }
}

export function TaskResults({task, files}: {task:Task;files:TaskFile[]}) {
  const {main,other,historical}=selectResultFiles(files,task.lifecycle?.execution_id)
  if(!main.length && !other.length) return null
  const publication=task.outcome?.publication
  return <section aria-label="最终作品" className="rounded-xl border border-primary/20 bg-card p-5 sm:p-7 space-y-5">
    <div><p className="text-xs text-muted-foreground mb-2">先看作品，再看创作过程</p><h2 className="text-xl font-semibold">{historical ? '已保留的作品' : '你的作品'}</h2><p className="text-sm text-muted-foreground mt-2">{task.status==='completed' ? '内容已生成，请预览并检查后再使用。' : '这里是已交付的内容，当前任务状态见上方提示。'}</p></div>
    {main.length > 0 ? <FilePreviewGallery previewTitle="作品预览" files={main} taskId={task.id} taskType={task.type} renderTrigger={(file,open)=><div className="flex flex-wrap items-center justify-between gap-4 rounded-lg bg-muted/40 p-4">
      <div className="flex items-center gap-3"><FileText className="size-7 text-primary"/><div><h3 className="font-medium">{file.mime_type.startsWith('video/') ? '最终视频' : file.mime_type==='text/html' ? '排版后的完整文章' : '完整正文'}{main.length>1 ? ` · ${main.indexOf(file)+1}` : ''}</h3><p className="text-xs text-muted-foreground mt-1">打开即可阅读、复制或下载</p></div></div>
      <Button onClick={open}><Eye className="size-4"/>查看作品</Button>
    </div>}/> : <p className="text-sm text-muted-foreground">本次作品见下方文件。暂未识别到单独的完整正文。</p>}
    {publication && ['blocked','failed','ambiguous'].includes(publication.status) && <div role="note" className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><strong>公众号交付还需处理</strong><p className="mt-1">{publication.status==='ambiguous' ? '公众号端的结果尚待核实，请先检查，避免重复提交。' : '内容可先查看；公众号草稿或发布步骤尚未成功，请展开下方过程查看原因。'}</p></div>}
    {!!other.length && <details className="group"><summary className="cursor-pointer text-sm text-muted-foreground py-2">配图与其他交付文件（{other.length}）</summary><div className="mt-3 grid gap-3 sm:grid-cols-2"><FilePreviewGallery files={other} taskId={task.id} taskType={task.type} compact/></div></details>}
  </section>
}
