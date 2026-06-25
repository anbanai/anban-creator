export type CreditTransactionType =
  | 'sign_in'
  | 'task_deduct'
  | 'task_refund'
  | 'admin_grant'
  | 'image_gen'
  | 'image_upload'
  | 'article_write'
  | 'convert'
  | 'humanize'
  | 'topic_research'
  | 'seo'
  | 'draft_publish'
  | 'outline'
  | 'viral_analysis'

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

export interface CreditPricing {
  task_costs: Record<string, number>
  model_costs: Record<string, Record<string, number>>
  // E-commerce module unit prices (key → credits per unit). A task's package
  // cost = Σ(price × quantity) over selected_modules. Absent on older servers.
  ecommerce_module_prices?: Record<string, number>
  income: {
    daily_sign_in: number
    register_bonus: number
    invite_reward: number
  }
}
