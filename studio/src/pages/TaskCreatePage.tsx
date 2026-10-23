import { useNavigate, useSearchParams } from 'react-router-dom'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { parseCreationIntent } from '@/lib/command-center'

export default function TaskCreatePage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { projectId, type } = parseCreationIntent(params)
  return <div className="mx-auto w-full max-w-5xl p-4 md:p-8">
    <TaskFormDialog key={`${projectId ?? ''}:${type ?? ''}`} open presentation="page" mode="create" initialProjectId={projectId} initialType={type}
      onOpenChange={open => { if (!open) navigate(projectId ? `/portraits/${encodeURIComponent(projectId)}` : '/tasks') }}
      onCreated={task => navigate(`/tasks/${task.id}`)} />
  </div>
}
