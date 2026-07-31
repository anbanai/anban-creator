export interface BillingWallet {
  paid: number
  promotional: number
  debt: number
  balance: number
}

export type BillingWalletEventKind =
  | 'topup'
  | 'promotion'
  | 'charge'
  | 'debt_created'
  | 'debt_repayment'
  | 'reversal'
  | 'expiry'

export interface BillingTransaction {
  id: string
  event_kind: BillingWalletEventKind
  paid_delta: number
  promotional_delta: number
  debt_delta: number
  catalog_id?: string
  charge_id?: string
  lot_id?: string
  resource_type?: string
  resource_id?: string
  source_type?: string
  source_id?: string
  charge_kind?: 'task' | 'operation' | 'reversal'
  charge_policy?: string
  sku_id?: string
  price_credits?: number
  pricing_tier?: 'free' | 'pro' | 'enterprise'
  list_price_credits?: number
  discount_credits?: number
  charge_resource_type?: string
  charge_resource_id?: string
  task_id?: string
  operation_task_id?: string
  tool_call_id?: string
  topup_credits?: number
  debt_repaid_credits?: number
  created_at: string
}

export interface BillingTransactions {
  items: BillingTransaction[]
  total: number
  offset: number
  limit: number
}

export interface BillingSKU {
  id: string
  operation: string
  charge_policy: 'task_admission' | 'accepted_task_operation' | 'standalone_operation'
  price_credits: number
  peak_price_credits?: number
  off_peak_price_credits?: number
  pricing_tier?: 'free' | 'pro' | 'enterprise'
  list_price_credits?: number
  discount_credits?: number
  execution_profile?: import('./agent-profile').AgentExecutionProfileID
  route?: string
  delivery: string
}

export interface BillingTimeWindow { start: string; end: string }

export interface TaskTimePricing {
  timezone: string
  peak_windows: BillingTimeWindow[]
  off_peak_windows: BillingTimeWindow[]
  off_peak_rate_percent: number
  current_period: 'peak' | 'off_peak'
  server_time: string
  next_transition_at: string
}

export interface BillingCatalog {
  catalog_id: string
  currency: 'credits'
  pricing_tier?: 'free' | 'pro' | 'enterprise'
  task_time_pricing?: TaskTimePricing
  skus: BillingSKU[]
}

export interface ScheduleRecommendation {
  time: string
  timezone: string
  granularity_minutes: number
  load_balanced: boolean
}

export interface BillingReferralProgram {
  id: string
  catalog_id: string
  minimum_topup_credits: number
  inviter_credits: number
  invitee_credits: number
  expires_after_seconds: number
  max_inviter_rewards: number
}

export interface BillingReferral {
  invite_code: string
  invite_link: string
  status: 'not_issued' | 'issued' | 'capped'
  program: BillingReferralProgram | null
  issue_id?: string
  issued_at?: string
}
