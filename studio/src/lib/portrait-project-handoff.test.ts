import { describe, expect, it } from 'vitest'
import { portraitCreationBrief, portraitToProjectProfile, preparePortraitProject } from './portrait-project-handoff'
import type { PortraitCandidate } from './portrait-chat-contract'
import type { ProjectProfile } from '@/types'

const input = '我是花店主，面向上班族，用公众号分享花艺。第一篇写周末插花。'
const fact = (text: string) => ({text, certainty:'stated' as const,evidence:[{messageId:'u-1',quote:text}]})
const candidate: PortraitCandidate = { reply:'请核对画像。',name:null,summary:'花店主，为上班族分享花艺。',facets:{identity:fact('花店主'),audience:fact('上班族'),platforms:fact('公众号'),style:null,preferences:null,experience:null},creationIdea:fact('第一篇写周末插花') }

describe('portrait handoff to existing project contracts',()=>{
  it('separates a single work from the long-term project and does not invent a platform binding',()=>{
    const prepared=preparePortraitProject(candidate,[{id:'u-1',role:'user',text:input}])
    expect(prepared.project.platform).toBeUndefined()
    expect(JSON.stringify(prepared.project)).not.toContain('周末插花')
    expect(portraitCreationBrief(candidate)).toContain('周末插花')
    expect(()=>portraitCreationBrief({...candidate,creationIdea:null})).toThrow()
  })
  it('preserves server revision metadata and maps experience to memory without copying task intent',()=>{
    const current={schema_version:1,version:7,status:'draft',initialization_status:'not_started',analysis_task_id:'server-task'} as ProjectProfile
    const profile=portraitToProjectProfile(candidate,current)
    expect(profile.version).toBe(7)
    expect(profile.analysis_task_id).toBe('server-task')
    expect(profile.status).toBe('draft')
    expect(profile.dimensions.identity.sources).toEqual(['[用户确认]'])
    expect(profile.dimensions.memory.content).toEqual({})
    expect(JSON.stringify(profile)).not.toContain('周末插花')
  })
  it('rejects unsupported or incomplete portraits before preparing a project',()=>{
    expect(()=>preparePortraitProject(candidate,[{id:'u-1',role:'user',text:'从未提供这些信息'}])).toThrow()
    expect(()=>preparePortraitProject({...candidate,facets:{...candidate.facets,platforms:null}},[{id:'u-1',role:'user',text:input}])).toThrow()
  })
})
