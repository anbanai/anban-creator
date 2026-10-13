import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { uploadToOSS } from '@/lib/direct-upload'
import { wechatAccountAnalyticsImportApi } from '@/lib/api/wechat-account-analytics-import'
import type { Project } from '@/types'
import WechatAccountImportDialog from './WechatAccountImportDialog'

vi.mock('@/lib/direct-upload', () => ({ uploadToOSS: vi.fn() }))
vi.mock('@/lib/api/wechat-account-analytics-import', () => ({ wechatAccountAnalyticsImportApi: { preview: vi.fn(), import: vi.fn() } }))

const project = { id: 'wechat-1', name: '公众号项目', platform: 'wechat' } as Project

describe('公众号账号导入类型切换', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(uploadToOSS).mockResolvedValue({ uploadSessionId: 'upload-1' } as never)
    vi.mocked(wechatAccountAnalyticsImportApi.preview).mockRejectedValue(Object.assign(new Error('Request failed with status code 400'), {
      response: { data: { code: 40000, msg: 'unsupported WeChat account analytics workbook: 检测到公众号内容分析报表，请在“内容表现”区域使用“导入单篇内容”；账号导入需要账号每日统计表' } },
    }))
  })

  it('hands an article cumulative workbook to the content importer without requiring another upload', async () => {
    const onContentImportRequested = vi.fn()
    const file = new File(['content'], 'total.xls')
    render(<WechatAccountImportDialog project={project} onClose={vi.fn()} onContentImportRequested={onContentImportRequested} />)

    fireEvent.change(screen.getByLabelText('选择账号统计文件'), { target: { files: [file] } })
    await waitFor(() => expect(onContentImportRequested).toHaveBeenCalledWith({ file, uploadId: 'upload-1' }))
    expect(screen.queryByText('文件解析失败，请重试')).not.toBeInTheDocument()
  })
})
