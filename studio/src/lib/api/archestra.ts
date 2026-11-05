import { http, unwrap } from '@/lib/http-client'
import type { ArchestraAgent, ArchestraSendResult, ArchestraTask } from '@/types/archestra'

export const archestraApi = {
  agents: () => unwrap<ArchestraAgent[]>(http.get('/admin/archestra/agents')),
  sendMessage: (agentId: string, payload: { context_id?: string; task_id?: string; text: string }) =>
    unwrap<ArchestraSendResult>(http.post(`/admin/archestra/agents/${encodeURIComponent(agentId)}/messages`, payload)),
  task: (taskId: string, agentId: string) =>
    unwrap<ArchestraTask>(http.get(`/admin/archestra/tasks/${encodeURIComponent(taskId)}`, { params: { agent_id: agentId } })),
  approval: (taskId: string, payload: { agent_id: string; context_id?: string; approval_id: string; approved: boolean }) =>
    unwrap<ArchestraSendResult>(http.post(`/admin/archestra/tasks/${encodeURIComponent(taskId)}/approval`, payload)),
  cancel: (taskId: string, agentId: string) =>
    unwrap<ArchestraTask>(http.post(`/admin/archestra/tasks/${encodeURIComponent(taskId)}/cancel`, { agent_id: agentId })),
}
