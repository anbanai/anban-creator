import { http, unwrap } from '@/lib/http-client'
import type { BillingCatalog, BillingReferral, BillingTransactions, BillingWallet } from '@/types'

export const billingApi = {
  wallet: () =>
    unwrap<BillingWallet>(http.get('/billing/wallet')),

  catalog: () =>
    unwrap<BillingCatalog>(http.get('/billing/catalog')),

  transactions: (params?: { offset?: number; limit?: number }) =>
    unwrap<BillingTransactions>(http.get('/billing/transactions', { params })),

  referral: () =>
    unwrap<BillingReferral>(http.get('/billing/referral')),
}
