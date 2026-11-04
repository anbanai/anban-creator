import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { TaskFormDialog } from '@/components/tasks/TaskFormDialog'
import { parseCreationIntent } from '@/lib/command-center'
import { parseMobileTaskDraft } from '@/lib/mobile-creation'

export default function TaskCreatePage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { projectId, type } = parseCreationIntent(params)
  const mobileDraft = parseMobileTaskDraft(location.state, projectId, type)
  return <div className="mobile-task-create mx-auto w-full max-w-5xl p-4 md:p-8">
    <TaskFormDialog key={`${projectId ?? ''}:${type ?? ''}:${location.key}`} open presentation="page" mode="create" initialProjectId={projectId} initialType={type} initialDraft={mobileDraft}
      onOpenChange={open => { if (!open) navigate(projectId ? `/portraits/${encodeURIComponent(projectId)}` : '/tasks') }}
      onCreated={task => navigate(`/tasks/${task.id}`)} />
  </div>
}
