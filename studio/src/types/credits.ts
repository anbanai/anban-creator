export type CreditTransactionType = 'sign_in' | 'task_deduct' | 'task_refund' | 'admin_grant'

export interface CreditBalance {
  balance: number
}

export interface SignInStatus {
  signed_in_today: boolean
}

export interface CreditTransaction {
  id: number
  user_id: string
  type: CreditTransactionType
  amount: number
  balance_after: number
  task_id?: string
  description: string
  created_at: string
}

export interface AdminGrantRequest {
  user_id: string
  amount: number
  description: string
}
