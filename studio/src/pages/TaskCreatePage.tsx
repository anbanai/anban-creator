import { useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { parseCreationIntent } from '@/lib/command-center'

export default function TaskCreatePage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const created = useRef(false)
  const { projectId, type } = parseCreationIntent(params)
  return <div className="mx-auto w-full max-w-5xl p-4 md:p-8">
    <TaskFormDialog key={`${projectId ?? ''}:${type ?? ''}:${params.get('asset_id') ?? ''}`} open presentation="page" mode="create" initialProjectId={projectId} initialType={type} initialAssetId={params.get('asset_id') ?? undefined} initialPrompt={params.get('prompt') ?? undefined}
      onOpenChange={open => { if (!open && !created.current) navigate(projectId ? `/portraits/${encodeURIComponent(projectId)}` : '/tasks') }}
      onCreated={task => { created.current = true; navigate(`/tasks/${task.id}`) }} />
  </div>
}
