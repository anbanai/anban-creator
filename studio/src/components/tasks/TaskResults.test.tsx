import { describe,it,expect } from 'vitest'
import { selectResultFiles } from './TaskResults'
import type { TaskFile } from '@/types'
const file=(id:string,extra:Partial<TaskFile>={}):TaskFile=>({id,task_id:'t',state:'delivered',role:'artifact',file_name:`${id}.md`,mime_type:'text/markdown',file_size:10,url:'',created_at:'',is_deliverable:true,...extra})
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
})
