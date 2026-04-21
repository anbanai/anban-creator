import { http, unwrap } from '@/lib/http-client'
import type { CreditBalance, SignInStatus, CreditTransaction, PaginatedResponse } from '@/types'

export const creditsApi = {
  balance: () =>
    unwrap<CreditBalance>(http.get('/credits/balance')),

  signInStatus: () =>
    unwrap<SignInStatus>(http.get('/credits/sign-in/status')),

  signIn: () =>
    unwrap<CreditBalance>(http.post('/credits/sign-in')),

  transactions: (params?: { page?: number; page_size?: number }) =>
    unwrap<PaginatedResponse<CreditTransaction>>(http.get('/credits/transactions', { params })),
}
