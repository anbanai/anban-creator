import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import TopicNotificationSettings from './TopicNotificationSettings'
const { channels, updateChannel, testChannel } = vi.hoisted(() => ({ channels: vi.fn(), updateChannel: vi.fn(), testChannel: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { projects: { list: async () => [{id:'p',name:'项目'}] }, topicRadar: {channels, updateChannel, testChannel} } }))
vi.mock('sonner', () => ({ toast: { success:vi.fn(), error:vi.fn() } }))
beforeEach(() => { vi.clearAllMocks(); channels.mockResolvedValue({items:[{id:'c',name:'日报',provider:'feishu',enabled:true}]}); updateChannel.mockResolvedValue({id:'c'}); testChannel.mockResolvedValue({sent:true}) })
it('preserves an existing secret and sends only an explicit test request',async () => {
 render(<TopicNotificationSettings />)
 fireEvent.click(screen.getByRole('button',{name:'管理通知渠道'}))
 await screen.findByText('日报')
 expect(testChannel).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'编辑'}))
 expect(screen.getByLabelText('Webhook URL')).toHaveValue('')
 fireEvent.click(screen.getByRole('button',{name:'保存渠道'}))
 await waitFor(() => expect(updateChannel).toHaveBeenCalledWith('c',{name:'日报',provider:'feishu',enabled:true}))
 fireEvent.click(screen.getByRole('button',{name:'发送测试'}))
 await waitFor(() => expect(testChannel).toHaveBeenCalledWith('c'))
})
