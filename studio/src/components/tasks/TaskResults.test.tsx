import { describe,it,expect } from 'vitest'
import { screen } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { selectResultFiles, TaskResults } from './TaskResults'
import type { Task, TaskFile } from '@/types'
const file=(id:string,extra:Partial<TaskFile>={}):TaskFile=>({id,task_id:'t',state:'delivered',role:'artifact',file_name:`${id}.md`,mime_type:'text/markdown',file_size:10,url:'',created_at:'',is_deliverable:true,...extra})
const task={id:'t',type:'video-edit',prompt:'cut this interview',status:'completed',project_id:'p',execution_profile:'effective',billing_price_credits:0,created_at:'',started_at:'',completed_at:''} as Task
describe('result selection',()=>{
  it('foregrounds delivered HTML, keeps source and images accessible, and never promotes process files',()=>{
    const result=selectResultFiles([file('research',{is_deliverable:false}),file('article-final'),file('article',{mime_type:'text/html'}),file('cover',{mime_type:'image/png'}),file('failed-html',{state:'retained',mime_type:'text/html'})])
    expect(result.main.map(f=>f.id)).toEqual(['article'])
    expect(result.other.map(f=>f.id)).toEqual(['article-final','cover'])
  })
  it('prefers current execution content over historical HTML and labels historical fallback',()=>{
    const old=file('old',{mime_type:'text/html',execution_id:'old'})
    const current=file('article-final',{execution_id:'new'})
    expect(selectResultFiles([old,current],'new').main).toEqual([current])
    expect(selectResultFiles([old],'new').historical).toBe(true)
  })
  it('keeps multiple final documents rather than guessing a winner',()=>{
    expect(selectResultFiles([file('one',{mime_type:'text/html'}),file('two',{mime_type:'text/html'})]).main).toHaveLength(2)
  })

  it('shows the final video first and keeps video-edit evidence available in the same review', () => {
    const files = [
      file('final', { file_name: 'final.mp4', mime_type: 'video/mp4', role: 'final_video', delivery_role: 'final_video', preview_url: 'https://assets.example/final.mp4' }),
      file('edl', { file_name: 'edit-decision-list.json', mime_type: 'application/json', delivery_role: 'edit_decision_list' }),
      file('strategy', { file_name: 'edit-strategy.md', mime_type: 'text/markdown', delivery_role: 'edit_strategy' }),
      file('subtitles', { file_name: 'subtitles.srt', mime_type: 'application/x-subrip', delivery_role: 'subtitles' }),
      file('tags', { file_name: 'content-tags.json', mime_type: 'application/json', delivery_role: 'content_tags' }),
      file('quality', { file_name: 'quality-evidence.json', mime_type: 'application/json', delivery_role: 'quality_evidence' }),
    ]

    render(<TaskResults task={task} files={files} />)

    expect(screen.getByRole('heading', { name: '你的作品' })).toBeInTheDocument()
    expect(screen.getByText(/剪辑清单 · edit-decision-list.json/)).toBeInTheDocument()
    expect(screen.getByText(/剪辑策略 · edit-strategy.md/)).toBeInTheDocument()
    expect(screen.getByText(/字幕 · subtitles.srt/)).toBeInTheDocument()
    expect(screen.getByText(/内容标签 · content-tags.json/)).toBeInTheDocument()
    expect(screen.getByText(/质量证据 · quality-evidence.json/)).toBeInTheDocument()
  })
})
