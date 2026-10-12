import { http, unwrap } from '@/lib/http-client'
import type { AdminBillingUser, AdminTopUpOperations, AdminWalletEntries, BillingCatalog, BillingReferral, BillingTransactions, BillingWallet } from '@/types'

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

export const adminBillingApi = {
  users: (params?: { q?: string; limit?: number }) =>
    unwrap<{ items: AdminBillingUser[] }>(http.get('/admin/billing/users', { params })),

  createTopUp: (payload: { user_id: string; credits: number; external_source_id: string }) =>
    unwrap<{ status: 'succeeded' | 'replayed'; entry_id?: string; paid_credits_added?: number; debt_repaid_credits?: number; operation?: AdminTopUpOperations['items'][number] }>(http.post('/admin/billing/topups', payload)),

  topups: (params?: { user_id?: string; status?: string; external_source_id?: string; offset?: number; limit?: number; from?: string; to?: string }) =>
    unwrap<AdminTopUpOperations>(http.get('/admin/billing/topups', { params })),

  entries: (params?: { user_id?: string; event_kind?: string; offset?: number; limit?: number }) =>
    unwrap<AdminWalletEntries>(http.get('/admin/billing/entries', { params })),
}
