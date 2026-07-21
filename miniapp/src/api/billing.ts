import { get } from './request'
import type { BillingCatalog, BillingReferral, BillingTransactions, BillingWallet } from '@/types'

export const billingApi = {
  wallet: () => get<BillingWallet>('/billing/wallet'),
  catalog: () => get<BillingCatalog>('/billing/catalog'),
  transactions: (params?: { offset?: number; limit?: number }) =>
    get<BillingTransactions>('/billing/transactions', params as Record<string, any>),
  referral: () => get<BillingReferral>('/billing/referral'),
}
