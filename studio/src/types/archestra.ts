export interface ArchestraAgent {
  id: string
  name: string
  description?: string
}

export interface ArchestraMessage {
  role: string
  context_id?: string
  text?: string
}

export interface ArchestraApprovalRequest {
  approval_id: string
  tool_name?: string
  text?: string
  expires_at?: string
  resolved?: boolean
}

export interface ArchestraTask {
  task_id: string
  context_id?: string
  state: string
  status_message?: string
  response?: string
  approval_requests?: ArchestraApprovalRequest[]
}

export interface ArchestraSendResult {
  message?: ArchestraMessage
  task?: ArchestraTask
}
