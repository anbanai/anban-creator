export type CreditTransactionType =
  | 'sign_in'
  | 'task_deduct'
  | 'task_refund'
  | 'admin_grant'
  | 'register_bonus'
  | 'invite_reward'
  | 'image_gen'
  | 'image_understanding'
  | 'image_upload'
  | 'article_write'
  | 'convert'
  | 'humanize'
  | 'topic_research'
  | 'seo'
  | 'draft_publish'
  | 'outline'
  | 'viral_analysis'
  | 'video_gen'
  | 'video_understanding'
  | 'poster_generation'

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
  operation_id?: string
  description: string
  metadata?: {
    provider?: string
    model?: string
    route?: string
    input_tokens?: number
    cached_input_tokens?: number
    output_tokens?: number
    text_input_tokens?: number
    text_cached_input_tokens?: number
    image_input_tokens?: number
    image_cached_input_tokens?: number
    image_output_tokens?: number
    total_tokens?: number
    base_credits?: number
    tier_multiplier?: number
    user_multiplier?: number
    final_credits?: number
    price_snapshot?: Record<string, unknown>
  }
  created_at: string
}

export interface AdminGrantRequest {
  user_id: string
  amount: number
  description: string
}

export interface RechargeTier {
  key: string
  label: string
  price_cny: number
  credits: number
  bonus_credits?: number
  enabled?: boolean
}

export interface CreditPricing {
  task_costs: Record<string, number>
  model_costs: Record<string, Record<string, number>>
  recharge_tiers?: RechargeTier[]
  model_prices?: {
    currency_rates?: Record<string, { to_cny: number }>
    token_models?: Record<string, Record<string, number | string>>
    image_generation?: Record<string, Record<string, unknown>>
    video_generation?: Record<string, Record<string, unknown>>
  }
  billing?: {
    credits_per_cny?: number
    tier_multipliers?: Record<string, number>
    default_user_multiplier?: number
    minimum_charge_credits?: number
  }
  // E-commerce module unit prices (key → credits per unit). A task's package
  // cost = Σ(price × quantity) over selected_modules. Absent on older servers.
  ecommerce_module_prices?: Record<string, number>
  income?: {
    daily_sign_in: number
    register_bonus: number
    invite_reward: number
  }
}
