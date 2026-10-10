import { describe, expect, it, vi } from 'vitest'
import { portraitKey, savePortraitProject, type PortraitReceipt } from './portrait-online-save'
import { portraitToProjectProfile } from './portrait-project-handoff'
import type { PortraitCandidate } from './portrait-chat-contract'
import type { ProjectProfile, CreateProjectResponse } from '@/types'
const text='我是花店主，服务上班族，做公众号。'
const fact={text,certainty:'stated' as const,evidence:[{messageId:'u-1',quote:text}]}
const candidate:PortraitCandidate={reply:'请确认',name:null,summary:null,facets:{identity:fact,audience:fact,platforms:fact,style:null,preferences:null,experience:null},creationIdea:null}
const messages=[{id:'u-1',role:'user' as const,text}]
const initial:PortraitReceipt={userId:'user-1',portraitKey:portraitKey(candidate),phase:'new'}
const current={schema_version:1,status:'draft',version:0,initialization_status:'not_started'} as ProjectProfile
function fixture(){
  const confirmed={...portraitToProjectProfile(candidate,current),status:'confirmed' as const,version:1}
  const client={create:vi.fn(async()=>({project:{id:'project-1'}} as CreateProjectResponse)),getAccountProfile:vi.fn(async()=>current),confirmAccountProfile:vi.fn(async()=>confirmed)}
  const checkpoints:PortraitReceipt[]=[]
  return {client,confirmed,checkpoints,checkpoint:(receipt:PortraitReceipt)=>checkpoints.push(receipt)}
}
describe('online portrait save and recovery',()=>{
  it('saves later interview updates into the same project with a version check',async()=>{
    const f=fixture()
    const changed={...candidate,facets:{...candidate.facets,audience:null}}
    f.client.getAccountProfile.mockResolvedValue(f.confirmed)
    f.client.confirmAccountProfile.mockResolvedValue({...portraitToProjectProfile(changed,f.confirmed),status:'confirmed',version:2})
    const result=await savePortraitProject(f.client,changed,messages,{...initial,phase:'saved',projectId:'project-1',profileVersion:1},f.checkpoint)
    expect(f.client.create).not.toHaveBeenCalled()
    expect(f.client.confirmAccountProfile).toHaveBeenCalledWith('project-1',expect.objectContaining({version:1}))
    expect(result.profileVersion).toBe(2)
    expect(result.portraitKey).toBe(portraitKey(changed))
  })
  it('does not overwrite a newer server revision when saving a changed portrait',async()=>{
    const f=fixture()
    f.client.getAccountProfile.mockResolvedValue({...f.confirmed,version:3})
    await expect(savePortraitProject(f.client,{...candidate,facets:{...candidate.facets,audience:null}},messages,{...initial,phase:'saved',projectId:'project-1',profileVersion:1},f.checkpoint)).rejects.toThrow('新版本')
    expect(f.client.confirmAccountProfile).not.toHaveBeenCalled()
  })
  it('creates once, records project identity, then confirms using the Server revision',async()=>{
    const f=fixture();const result=await savePortraitProject(f.client,candidate,messages,initial,f.checkpoint)
    expect(f.checkpoints.map(r=>r.phase)).toEqual(['creating','project_created','saving','saved'])
    expect(result.projectId).toBe('project-1');expect(result.profileVersion).toBe(1)
    expect(f.client.confirmAccountProfile).toHaveBeenCalledWith('project-1',expect.objectContaining({version:0}))
  })
  it('does not create again after an ambiguous create response',async()=>{
    const f=fixture();f.client.create.mockRejectedValueOnce(new Error('connection lost'))
    await expect(savePortraitProject(f.client,candidate,messages,initial,f.checkpoint)).rejects.toThrow()
    await expect(savePortraitProject(f.client,candidate,messages,f.checkpoints[0],f.checkpoint)).rejects.toThrow('暂不重复创建')
    expect(f.client.create).toHaveBeenCalledTimes(1)
  })
  it('retains the project on profile failure and recovers an already-saved confirmation by reading Server',async()=>{
    const f=fixture();f.client.confirmAccountProfile.mockRejectedValueOnce(new Error('response lost'))
    await expect(savePortraitProject(f.client,candidate,messages,initial,f.checkpoint)).rejects.toThrow()
    f.client.getAccountProfile.mockResolvedValueOnce(f.confirmed)
    const result=await savePortraitProject(f.client,candidate,messages,f.checkpoints[f.checkpoints.length-1],f.checkpoint)
    expect(result.phase).toBe('saved');expect(f.client.create).toHaveBeenCalledTimes(1);expect(f.client.confirmAccountProfile).toHaveBeenCalledTimes(1)
  })
  it('does not overwrite a concurrent edit or silently use a different portrait',async()=>{
    const f=fixture();f.client.getAccountProfile.mockResolvedValueOnce({...current,version:2})
    await expect(savePortraitProject(f.client,candidate,messages,{...initial,phase:'saving',projectId:'project-1',baseRevision:0},f.checkpoint)).rejects.toThrow('新版本')
    await expect(savePortraitProject(f.client,candidate,messages,{...initial,portraitKey:'different',phase:'saving',projectId:'project-1'},f.checkpoint)).rejects.toThrow('尚未确认')
    expect(f.client.create).not.toHaveBeenCalled();expect(f.client.confirmAccountProfile).not.toHaveBeenCalled()
  })
})
