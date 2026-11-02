import { expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { render } from '@/test/test-utils'
import TopicPoolPage from './TopicPoolPage'
vi.mock('@/lib/api',() => ({api:{projects:{list:async()=>[{id:'p',name:'项目'}]},topicPool:{list:async()=>({total:1,items:[{id:3,topic:'自动入库选题',status:'unused',source_label:'选题雷达',source_digest_id:'d',source_suggestion_id:'s',created_at:'2026-10-31T09:00:00Z'}]})}}}))
it('links automatically adopted topics to their frozen radar evidence',async()=>{
 render(<TopicPoolPage />)
 expect(await screen.findByText('自动入库选题')).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'查看雷达证据'})).toHaveAttribute('href','/topic-radar?project_id=p&digest_id=d')
 expect(screen.getByText('未使用')).toBeInTheDocument()
})
