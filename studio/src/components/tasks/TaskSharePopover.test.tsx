import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { TaskSharePopover } from './TaskSharePopover'

const active = { status: 'active', share_path: '/share/current', expires_at: '2030-11-04T12:00:00Z' }

describe('TaskSharePopover', () => {
  beforeEach(() => {
    server.use(http.get('/api/v1/tasks/task-1/share', () => HttpResponse.json({ data: { status: 'none' } })))
  })

  it('loads without creating; creates seven days, copies, updates, revokes and reloads on reopening', async () => {
    let current = { status: 'none' } as typeof active
    const days: number[] = []
    server.use(
      http.get('/api/v1/tasks/task-1/share', () => HttpResponse.json({ data: current })),
      http.put('/api/v1/tasks/task-1/share', async ({ request }) => {
        days.push((await request.json() as { expires_in_days: number }).expires_in_days)
        current = active
        return HttpResponse.json({ data: active })
      }),
      http.delete('/api/v1/tasks/task-1/share', () => { current = { ...active, status: 'revoked' }; return new HttpResponse(null, { status: 204 }) }),
    )
    const clipboard = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: clipboard } })
    render(<TaskSharePopover taskId="task-1" canCreate />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    await screen.findByRole('button', { name: '创建链接' })
    expect(screen.getByRole('dialog', { name: '分享作品' })).toBeVisible()
    expect(days).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: '创建链接' }))
    await screen.findByDisplayValue('http://localhost:3000/share/current')
    expect(days).toEqual([7])
    fireEvent.click(screen.getByRole('button', { name: '复制链接' }))
    await screen.findByText('已复制')
    expect(clipboard).toHaveBeenCalledWith('http://localhost:3000/share/current')
    fireEvent.click(screen.getByLabelText('1 天'))
    fireEvent.click(screen.getByRole('button', { name: '更新期限' }))
    await waitFor(() => expect(days).toEqual([7, 1]))
    fireEvent.click(screen.getByRole('button', { name: '关闭分享' }))
    await screen.findByText('分享已关闭')
    fireEvent.click(screen.getByRole('button', { name: '关闭分享设置' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await waitFor(() => expect(screen.getByRole('button', { name: '分享作品' })).toHaveFocus())
    current = active
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    await screen.findByRole('button', { name: '复制链接' })
  })

  it('rejects fractional and out of range custom days, then retries a failed save', async () => {
    let requests = 0
    server.use(http.put('/api/v1/tasks/task-1/share', () => {
      requests++
      return requests === 1 ? new HttpResponse(null, { status: 500 }) : HttpResponse.json({ data: active })
    }))
    render(<TaskSharePopover taskId="task-1" canCreate />)
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    await screen.findByRole('button', { name: '创建链接' })
    fireEvent.click(screen.getByLabelText('自定义'))
    const input = screen.getByLabelText('有效天数')
    for (const value of ['1.5', '0', '366', '']) {
      fireEvent.change(input, { target: { value } })
      fireEvent.click(screen.getByRole('button', { name: '创建链接' }))
      expect(screen.getByRole('alert')).toHaveTextContent('1–365')
    }
    expect(requests).toBe(0)
    fireEvent.change(input, { target: { value: '30' } })
    fireEvent.click(screen.getByRole('button', { name: '创建链接' }))
    await screen.findByText('保存失败，请重试')
    fireEvent.click(screen.getByRole('button', { name: '创建链接' }))
    await screen.findByRole('button', { name: '复制链接' })
  })

  it('copies an existing active share without needing another delivered body', async () => {
    server.use(http.get('/api/v1/tasks/task-1/share', () => HttpResponse.json({ data: active })))
    render(<TaskSharePopover taskId="task-1" canCreate={false} />)
    fireEvent.click(screen.getByRole('button', { name: '分享作品' }))
    expect(await screen.findByRole('button', { name: '复制链接' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '更新期限' })).toBeDisabled()
  })
})
