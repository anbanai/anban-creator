import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ArrowLeft, ArrowRight, Check, UserRound } from 'lucide-react'
import { api } from '@/lib/api'
import { mobileCreations, mobileCreationType, mobileCreationHandoff, canUseMobileCreation, productImageChoices, newMobilePersonaState, MOBILE_BRIEF_MAX_LENGTH } from '@/lib/mobile-creation'
import { normalizeProjectTypeSelection, projectAgentIDs } from '@/lib/project-capabilities'
import { ProjectOutputSettingsDialog } from '@/components/projects/ProjectOutputSettingsDialog'
import { Button } from '@/components/ui/button'
import type { Project } from '@/types'

export default function MobileCreatePage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const projects = useQuery({ queryKey: ['projects', 'active'], queryFn: () => api.projects.list({ status: 'active' }) })
  const requestedId = params.get('project_id')
  const project = requestedId ? projects.data?.find(item => item.id === requestedId) : projects.data?.[0]
  const type = mobileCreationType(params.get('type')) ?? 'wechat-article'
  const config = mobileCreations.find(item => item.id === type)!
  const [brief, setBrief] = useState('')
  const [imageChoice, setImageChoice] = useState('hero')
  const [settingsProject, setSettingsProject] = useState<Project | null>(null)
  const [notice, setNotice] = useState('')
  const proceed = () => {
    if (!project) return
    if (!canUseMobileCreation(project, type)) { setSettingsProject(project); return }
    const destination = mobileCreationHandoff(project.id, type, brief, imageChoice)
    navigate(destination.to, { state: destination.state })
  }
  return <div className="mobile-studio-page mobile-create-page"><Link to="/mobile" className="mobile-back-link"><ArrowLeft size={16} />创作工作台</Link>
    <div className="mobile-page-eyebrow">FROM AN IDEA TO A CREATION</div><header className="mobile-page-title"><h1>{config.title}</h1><p>{type === 'ecommerce' ? '先选想做的图，再上传真实产品素材。' : type === 'video-edit' ? '说说想剪成什么样，再添加你的素材。' : '带着你的人设，把想法聊成一份作品。'}</p></header>
    <div role="group" aria-label="选择内容类型" className="mobile-type-tabs">{mobileCreations.map(item => <button type="button" key={item.id} aria-pressed={type === item.id} onClick={() => { setNotice(''); setParams(current => { const next = new URLSearchParams(current); next.set('type', item.id); return next }, { replace: true }) }}>{item.title}</button>)}</div>
    {projects.isPending ? <p role="status" className="mobile-empty-state">正在读取人设…</p> : projects.isError ? <div className="mobile-empty-state"><p role="alert">人设暂时无法读取</p><Button onClick={() => void projects.refetch()}>重试</Button></div> : <>
      <section className="mobile-form-card"><label htmlFor="mobile-create-persona"><UserRound size={16} />使用的人设</label><select id="mobile-create-persona" value={project?.id ?? ''} onChange={event => { setNotice(''); setParams(current => { const next = new URLSearchParams(current); next.set('project_id', event.target.value); return next }, { replace: true }) }}><option value="" disabled>请选择人设</option>{projects.data?.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>
        {requestedId && !project && <p role="alert" className="text-sm text-destructive">这个人设已不可用，请重新选择。</p>}
        {!projects.data?.length && <Link className="mobile-secondary-link" to="/" state={newMobilePersonaState}>先通过对话建立人设 <ArrowRight size={15} /></Link>}
      </section>
      {type === 'ecommerce' && <fieldset className="mobile-product-choices"><legend>这次需要什么图？</legend>{productImageChoices.map(choice => <button key={choice.id} type="button" aria-pressed={imageChoice === choice.id} onClick={() => setImageChoice(choice.id)}><span><strong>{choice.label}</strong><small>{choice.description}</small></span>{imageChoice === choice.id && <Check size={17} />}</button>)}</fieldset>}
      <section className="mobile-form-card"><label htmlFor="mobile-creation-brief">{type === 'ecommerce' ? '产品与图片要求' : '你的想法' }<span>可以先简单说说</span></label><textarea id="mobile-creation-brief" rows={4} maxLength={MOBILE_BRIEF_MAX_LENGTH} value={brief} onChange={event => setBrief(event.target.value)} placeholder={type === 'ecommerce' ? '例如：一款便携咖啡杯，突出轻巧和防漏；颜色以实拍为准。' : type === 'video-edit' ? '例如：把这段产品介绍剪成 30 秒短视频，保留重点，加上清晰字幕。' : '例如：想聊聊客户最关心的一个问题，语气自然，结合我的真实经历。'} /></section>
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      <div className="mobile-create-footer"><p>{type === 'wechat-article' || type === 'seednote' ? '先对话理清需求，确认后再生成。' : '下一步添加素材、确认设置，再创建作品。'}</p><Button disabled={!project} className="mobile-primary-link" onClick={proceed}>{type === 'wechat-article' || type === 'seednote' ? '进入创作对话' : '下一步，添加素材'}<ArrowRight size={17} /></Button></div>
    </>}
    {settingsProject && <ProjectOutputSettingsDialog key={`${settingsProject.id}:${type}`} project={settingsProject} initialAgentIDs={type === 'ecommerce' ? ['ecommerce'] : normalizeProjectTypeSelection(projectAgentIDs(settingsProject), [...projectAgentIDs(settingsProject), type])} syncAssistantPlatform onClose={() => setSettingsProject(null)} onSaved={saved => {
      setSettingsProject(null)
      if (!canUseMobileCreation(saved, type)) { setNotice('设置已保存。要继续当前创作，请启用对应的创作类型。'); return }
      const destination = mobileCreationHandoff(saved.id, type, brief, imageChoice)
      navigate(destination.to, { state: destination.state })
    }} />}
  </div>
}
