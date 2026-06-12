import { get, post } from './request'
import type { CreditBalance, SignInStatus, CreditTransaction, CreditPricing, PaginatedResponse } from '@/types'

export const creditsApi = {
  balance: () =>
    get<CreditBalance>('/credits/balance'),

  signInStatus: () =>
    get<SignInStatus>('/credits/sign-in/status'),

  signIn: () =>
    post<{ reward: number }>('/credits/sign-in'),

  transactions: (params?: { limit?: number; offset?: number }) =>
    get<PaginatedResponse<CreditTransaction>>('/credits/transactions', params as Record<string, any>),

  pricing: () =>
    get<CreditPricing>('/credits/pricing'),
}
