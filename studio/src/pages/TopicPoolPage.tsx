import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import PageHeader from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

export default function TopicPoolPage() {
  const [params] = useSearchParams()
  const [projectId,setProjectId] = useState(params.get('project_id') ?? '')
  const [status,setStatus] = useState('')
  const [offset,setOffset] = useState(0)
  const projectsQuery = useQuery({queryKey:queryKeys.projects.list({status:'active'}),queryFn:()=>api.projects.list({status:'active'})})
  const projects = projectsQuery.data ?? []
  const selectedProject = projects.some((project)=>project.id===projectId) ? projectId : projects[0]?.id ?? ''
  const topicsQuery = useQuery({queryKey:[...queryKeys.topicPool.list(selectedProject,status),offset],queryFn:()=>api.topicPool.list(selectedProject,{status:status || undefined,offset,limit:30}),enabled:Boolean(selectedProject)})
  return <div className="mx-auto max-w-6xl space-y-5 p-4 md:p-6"><PageHeader title="选题库" description="自动入库的选题、使用状态与来源证据" /><div className="flex flex-wrap items-center gap-3"><label className="grid gap-1 text-xs">项目<select className="h-9 rounded-md border bg-background px-2" value={selectedProject} onChange={(event)=>{setProjectId(event.target.value);setOffset(0)}}>{projects.map((project)=><option key={project.id} value={project.id}>{project.name}</option>)}</select></label><label className="grid gap-1 text-xs">使用状态<select className="h-9 rounded-md border bg-background px-2" value={status} onChange={(event)=>{setStatus(event.target.value);setOffset(0)}}><option value="">全部</option><option value="unused">待使用</option><option value="used">已使用</option></select></label><Link className="ml-auto text-sm underline" to={`/topic-radar?project_id=${selectedProject}`}>返回选题雷达</Link></div>
    {(projectsQuery.isLoading || (selectedProject && topicsQuery.isLoading)) && <p role="status">加载中…</p>}
    {(projectsQuery.isError || topicsQuery.isError) && <p role="alert" className="text-destructive">选题库加载失败，请重试。</p>}
    {!projectsQuery.isLoading && !projectsQuery.isError && projects.length === 0 && <p>暂无可用项目</p>}
    {topicsQuery.data?.items.length === 0 && <p className="py-8 text-sm text-muted-foreground">暂无选题，雷达分析完成后会自动入库。</p>}
    <div className="divide-y rounded-lg border">{topicsQuery.data?.items.map((topic)=><article key={topic.id} className="space-y-2 p-4"><div className="flex items-start justify-between gap-4"><h2 className="font-medium">{topic.topic}</h2><span className="shrink-0 text-xs text-muted-foreground">{topic.status === 'used' ? '已使用' : '未使用'}</span></div><div className="flex flex-wrap gap-4 text-xs text-muted-foreground"><span>{topic.source_label || '手动创建'}</span><time>{new Date(topic.created_at).toLocaleString()}</time>{topic.source_digest_id && <Link className="underline" to={`/topic-radar?project_id=${selectedProject}&digest_id=${topic.source_digest_id}`}>查看雷达证据</Link>}{topic.task_id && <Link className="underline" to={`/tasks/${topic.task_id}`}>查看创作任务</Link>}</div></article>)}</div>
    {topicsQuery.data && <div className="flex items-center justify-end gap-3 text-sm"><span>共 {topicsQuery.data.total} 条</span><Button variant="outline" disabled={offset===0} onClick={()=>setOffset(Math.max(0,offset-30))}>上一页</Button><Button variant="outline" disabled={offset+30>=topicsQuery.data.total} onClick={()=>setOffset(offset+30)}>下一页</Button></div>}
  </div>
}
