import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { Project } from '@/types'
import ContentImportHistory from './ContentImportHistory'
vi.mock('@/lib/api', () => ({ api: { wechatAnalyticsImport: { listBatches: vi.fn(), revoke: vi.fn() }, seednoteImport: { listBatches: vi.fn(), revoke: vi.fn() } } }))
beforeEach(() => vi.clearAllMocks())
for (const platform of ['article', 'seednote'] as const) describe(`${platform} 导入历史`, () => {
  it('confirms filename, retains error for retry, and refreshes revoked state', async () => {
    const method = platform === 'article' ? api.wechatAnalyticsImport : api.seednoteImport
    const batch = { id: 'b1', file_name: '数据.xlsx', data_as_of_at: '2026-09-24T12:00:00Z', status: 'completed', matched_rows: 2, resolved_rows: 2 }
    vi.mocked(method.listBatches).mockResolvedValue({ items: [batch], total: 1 } as never)
    vi.mocked(method.revoke).mockRejectedValueOnce(new Error('暂时失败')).mockImplementationOnce(async () => {
      vi.mocked(method.listBatches).mockResolvedValue({ items: [{ ...batch, status: 'revoked' }], total: 1 } as never)
      return {} as never
    })
    const onRevoked = vi.fn()
    render(<ContentImportHistory project={{ id: 'p1', name: '账号', platform } as Project} onClose={vi.fn()} onRevoked={onRevoked} />)
    fireEvent.click(await screen.findByRole('button', { name: '撤销本次导入' }))
    expect(method.revoke).not.toHaveBeenCalled()
    expect(screen.getByRole('region', { name: '确认撤销导入' })).toHaveTextContent('数据.xlsx')
    fireEvent.click(screen.getByRole('button', { name: '确认撤销' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('暂时失败')
    fireEvent.click(screen.getByRole('button', { name: '确认撤销' }))
    await waitFor(() => expect(onRevoked).toHaveBeenCalledOnce())
    expect(screen.getByText(/已撤销 · 不计入统计/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '撤销本次导入' })).not.toBeInTheDocument()
    expect(method.revoke).toHaveBeenCalledWith('p1', 'b1')
  })
})
