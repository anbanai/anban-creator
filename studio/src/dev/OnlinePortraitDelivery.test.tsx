import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OnlinePortraitDelivery from './OnlinePortraitDelivery'
import { api } from '@/lib/api'
import { portraitToProjectProfile } from '@/lib/portrait-project-handoff'
import type { PortraitCandidate } from '@/lib/portrait-chat-contract'
import type { ProjectProfile } from '@/types'
vi.mock('@/lib/api',()=>({api:{auth:{me:vi.fn()},projects:{create:vi.fn(),getAccountProfile:vi.fn(),confirmAccountProfile:vi.fn(),listChannelConfigs:vi.fn()},agentProfiles:{list:vi.fn()},billing:{catalog:vi.fn(),wallet:vi.fn()},tasks:{create:vi.fn(),get:vi.fn()}}}))
const text='我做花艺，面向上班族，用公众号分享；第一篇讲周末插花。'
const fact={text,certainty:'stated' as const,evidence:[{messageId:'u-1',quote:text}]}
const candidate:PortraitCandidate={reply:'请核对',name:null,summary:null,facets:{identity:fact,audience:fact,platforms:fact,style:null,preferences:null,experience:null},creationIdea:fact}
const messages=[{id:'u-1',role:'user' as const,text}]
const current={schema_version:1,version:0,status:'draft',initialization_status:'not_started'} as ProjectProfile
const confirmed={...portraitToProjectProfile(candidate,current),status:'confirmed' as const,version:1}
beforeEach(()=>{
  vi.resetAllMocks(); sessionStorage.clear()
  vi.mocked(api.auth.me).mockResolvedValue({id:'user-1',nickname:'测试账号'} as never)
  vi.mocked(api.agentProfiles.list).mockResolvedValue([{id:'effective',display_name:'性价比',available:true}] as never)
  vi.mocked(api.billing.catalog).mockResolvedValue({catalog_id:'v1',currency:'credits',skus:[{charge_policy:'task_admission',operation:'task.wechat_article',execution_profile:'effective',price_credits:30}]} as never)
  vi.mocked(api.billing.wallet).mockResolvedValue({balance:100} as never)
  vi.mocked(api.projects.create).mockResolvedValue({project:{id:'p-1'}} as never)
  vi.mocked(api.projects.getAccountProfile).mockResolvedValue(current)
  vi.mocked(api.projects.confirmAccountProfile).mockResolvedValue(confirmed)
  vi.mocked(api.projects.listChannelConfigs).mockResolvedValue([{id:'c-1',project_id:'p-1',channel:'wechat-article',config:{wechat_app_id:'wx-test'}}])
  vi.mocked(api.tasks.create).mockResolvedValue({id:'t-1',status:'pending'} as never)
  vi.mocked(api.tasks.get).mockResolvedValue({id:'t-1',status:'completed'} as never)
})
async function save(){
  await screen.findByText('当前账号：测试账号')
  fireEvent.click(screen.getByRole('button',{name:'保存项目和已确认画像'}))
  await screen.findByRole('button',{name:'生成第一篇作品'})
}
describe('online project and task delivery',()=>{
  it('blocks missing WeChat configuration before billing admission and allows retry after binding',async()=>{
    render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>);await save()
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(confirmed)
    vi.mocked(api.projects.listChannelConfigs).mockResolvedValue([])
    fireEvent.click(screen.getByRole('button',{name:'生成第一篇作品'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('绑定公众号')
    expect(api.tasks.create).not.toHaveBeenCalled()
    expect(JSON.parse(sessionStorage.getItem('anban:portrait-online:user-1')!).phase).toBe('saved')
    vi.mocked(api.projects.listChannelConfigs).mockResolvedValue([{id:'c-1',project_id:'p-1',channel:'wechat-article',config:{wechat_app_id:'wx-test'}}])
    fireEvent.click(screen.getByRole('button',{name:'生成第一篇作品'}))
    await screen.findByRole('link',{name:'查看进度与作品'})
    expect(api.tasks.create).toHaveBeenCalledTimes(1)
  })
  it('waits for explicit save and generation, shows actual task identity, and submits only one bounded task',async()=>{
    render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>)
    await screen.findByText('当前账号：测试账号')
    expect(api.projects.create).not.toHaveBeenCalled();expect(api.tasks.create).not.toHaveBeenCalled()
    await save();vi.mocked(api.projects.getAccountProfile).mockResolvedValue(confirmed)
    fireEvent.click(screen.getByRole('button',{name:'生成第一篇作品'}))
    await screen.findByRole('link',{name:'查看进度与作品'})
    expect(api.tasks.create).toHaveBeenCalledTimes(1)
    expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({project_id:'p-1',type:'wechat-article',quantity:1,execution_profile:'effective',article_with_cover:true,prompt:expect.stringContaining('不要自行公开发布')}))
    expect(screen.getByRole('link',{name:'查看进度与作品'})).toHaveAttribute('href','/tasks/t-1')
  })
  it('does not repeat an ambiguous create after remounting the local preview',async()=>{
    vi.mocked(api.projects.create).mockRejectedValue(new Error('lost response'))
    const view=render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>)
    await screen.findByText('当前账号：测试账号')
    fireEvent.click(screen.getByRole('button',{name:'保存项目和已确认画像'}))
    await screen.findByRole('alert');view.unmount()
    render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>)
    await screen.findByText('当前账号：测试账号')
    expect(screen.getByRole('button',{name:'保存项目和已确认画像'})).toBeDisabled()
    expect(api.projects.create).toHaveBeenCalledTimes(1)
  })
  it('blocks generation when the server profile has changed',async()=>{
    render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>);await save()
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue({...confirmed,version:2})
    fireEvent.click(screen.getByRole('button',{name:'生成第一篇作品'}))
    await screen.findByRole('alert');expect(api.tasks.create).not.toHaveBeenCalled()
  })
  it('does not retry an ambiguous task submission or erase the saved project',async()=>{
    render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>);await save()
    vi.mocked(api.projects.getAccountProfile).mockResolvedValue(confirmed)
    vi.mocked(api.tasks.create).mockRejectedValue(new Error('lost response'))
    fireEvent.click(screen.getByRole('button',{name:'生成第一篇作品'}))
    await screen.findByRole('alert')
    expect(screen.getByRole('button',{name:'生成第一篇作品'})).toBeDisabled()
    expect(screen.getByRole('link',{name:'查看线上项目'})).toHaveAttribute('href','/projects?edit=p-1')
  })
  it('keeps a late successful project save recoverable after leaving the page',async()=>{
    let resolve!: (value:never)=>void
    vi.mocked(api.projects.create).mockImplementation(()=>new Promise(done=>{resolve=done}))
    const view=render(<OnlinePortraitDelivery candidate={candidate} messages={messages}/>)
    await screen.findByText('当前账号：测试账号')
    fireEvent.click(screen.getByRole('button',{name:'保存项目和已确认画像'}))
    await waitFor(()=>expect(api.projects.create).toHaveBeenCalledTimes(1));view.unmount()
    await act(async()=>resolve({project:{id:'p-1'}} as never))
    expect(JSON.parse(sessionStorage.getItem('anban:portrait-online:user-1')!).phase).toBe('saved')
  })
})
