import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { portraitCreationBrief } from '@/lib/portrait-project-handoff'
import { portraitKey, savePortraitProject, type PortraitReceipt } from '@/lib/portrait-online-save'
import { taskCostFor, cheapestAvailableExecutionProfile } from '@/lib/pricing'
import { createTaskFormDefaults, taskFormValuesToRequest } from '@/lib/task-form'
import type { PortraitCandidate, PortraitChatMessage } from '@/lib/portrait-chat-contract'
import type { AgentExecutionProfileCapability, AgentExecutionProfileID, BillingCatalog, BillingWallet, Task } from '@/types'

type Account = { id:string; name:string; profiles:AgentExecutionProfileCapability[]; catalog:BillingCatalog; wallet:BillingWallet }
const receiptStorage = (userId:string) => `anban:portrait-online:${userId}`

/** Uses the existing authenticated Server APIs; never a local replacement project store. */
export default function OnlinePortraitDelivery({ candidate, messages }: {candidate:PortraitCandidate;messages:PortraitChatMessage[]}) {
  const [account,setAccount] = useState<Account|null>(null)
  const [receipt,setReceipt] = useState<PortraitReceipt|null>(null)
  const [checking,setChecking] = useState(false)
  const [busy,setBusy] = useState(false)
  const [error,setError] = useState('')
  const [task,setTask] = useState<Task|null>(null)
  const [type,setType] = useState<'wechat-article'|'seednote'>('wechat-article')
  const [profile,setProfile] = useState<AgentExecutionProfileID|''>('')
  const [images,setImages] = useState(true)
  const locked = useRef(false)
  const mounted = useRef(true)
  const key = portraitKey(candidate)
  function checkpoint(next:PortraitReceipt) {
    // This is only a recovery receipt. Project/profile/task truth stays on Server.
    sessionStorage.setItem(receiptStorage(next.userId), JSON.stringify(next))
    if (mounted.current) setReceipt(next)
  }
  async function connect() {
    if (locked.current) return
    setChecking(true); setError('')
    try {
      const user = await api.auth.me()
      const [profiles,catalog,wallet] = await Promise.all([api.agentProfiles.list(),api.billing.catalog(),api.billing.wallet()])
      if (!mounted.current) return
      setAccount({id:user.id,name:user.nickname || '当前账号',profiles,catalog,wallet})
      const raw = sessionStorage.getItem(receiptStorage(user.id))
      const saved = raw ? JSON.parse(raw) as PortraitReceipt : null
      if (saved && (saved.userId !== user.id || !['new','creating','project_created','saving','saved','task_creating','task_created'].includes(saved.phase))) throw new Error('本机恢复记录无效，请先核对线上项目。')
      setReceipt(saved ?? {userId:user.id,portraitKey:key,phase:'new'})
      setProfile(cheapestAvailableExecutionProfile(profiles,catalog,type) || '')
    } catch(problem) { if (mounted.current) setError(getApiErrorMessage(problem,'无法读取线上账号，请先登录后重新检查连接。')) }
    finally { if (mounted.current) setChecking(false) }
  }
  useEffect(()=>{ mounted.current=true; void connect(); return()=>{mounted.current=false} },[])
  useEffect(()=>{
    if (!receipt?.taskId) return
    let stopped=false
    let timer: ReturnType<typeof setTimeout>|undefined
    const poll=async()=>{
      try {
        const current=await api.tasks.get(receipt.taskId!)
        if(stopped) return
        setTask(current)
        if(current.status==='pending'||current.status==='running') timer=setTimeout(()=>void poll(),5000)
      } catch { if(!stopped) setError('任务已提交，但暂时无法读取进度。可打开作品任务查看，请勿重复生成。') }
    }
    void poll()
    return()=>{stopped=true;clearTimeout(timer)}
  },[receipt?.taskId])
  async function save() {
    if(locked.current||!account||!receipt) return
    locked.current=true;setBusy(true);setError('')
    try {
      const user=await api.auth.me()
      if(user.id!==account.id) throw new Error('登录账号发生变化，请重新检查连接。')
      await savePortraitProject(api.projects,candidate,messages,receipt,checkpoint)
    } catch(problem) { if(mounted.current) setError(getApiErrorMessage(problem,'保存未完成，已取得的项目编号会保留，请核对后继续。')) }
    finally {locked.current=false;if(mounted.current)setBusy(false)}
  }
  async function generate() {
    if(locked.current||!account||!receipt?.projectId||receipt.phase!=='saved'||!profile||receipt.portraitKey!==key) return
    locked.current=true;setBusy(true);setError('')
    try {
      const [user,current,catalog,wallet]=await Promise.all([api.auth.me(),api.projects.getAccountProfile(receipt.projectId),api.billing.catalog(),api.billing.wallet()])
      if(user.id!==account.id) throw new Error('登录账号发生变化，请重新检查连接。')
      if(current.status!=='confirmed'||current.version!==receipt.profileVersion) throw new Error('线上画像版本已变化，请先核对项目，不使用旧版本生成。')
      if(type==='wechat-article') {
        const channels=await api.projects.listChannelConfigs(receipt.projectId)
        const appId=channels.find(channel=>channel.channel==='wechat-article')?.config.wechat_app_id
        // Secrets are redacted by Server. This detects missing configuration,
        // but cannot certify credentials or WeChat permissions are valid.
        if(typeof appId!=='string'||!appId.trim()) throw new Error('请先在项目的「渠道配置」中绑定公众号，再回来生成。现有公众号流程需要读取历史文章；本次尚未提交任务或扣费。')
      }
      const cost=taskCostFor(catalog,type,profile)
      if(cost===undefined||cost!==taskCostFor(account.catalog,type,profile)||cost>wallet.balance) {
        setAccount({...account,catalog,wallet}); throw new Error('费用或余额已变化，请核对最新费用后再生成。')
      }
      const prompt=portraitCreationBrief(candidate)
      const request=taskFormValuesToRequest({...createTaskFormDefaults(),project_id:receipt.projectId,type,execution_profile:profile,prompt,quantity:1,article_with_cover:images,article_with_content_images:images})
      checkpoint({...receipt,phase:'task_creating'})
      const created=await api.tasks.create(request)
      if(!created?.id) throw new Error('未收到任务编号，请到任务列表核对，不要重复生成。')
      checkpoint({...receipt,phase:'task_created',taskId:created.id})
      if(mounted.current)setTask(created)
    } catch(problem) { if(mounted.current)setError(getApiErrorMessage(problem,'提交结果暂未确认，请到任务列表核对，避免重复扣费。')) }
    finally {locked.current=false;if(mounted.current)setBusy(false)}
  }
  const cost=account&&profile?taskCostFor(account.catalog,type,profile):undefined
  const saved=receipt?.profileVersion!==undefined
  const status=task?({pending:'已排队',running:'正在创作',completed:'任务已完成',failed:'生成失败',cancelled:'已取消'}[task.status]):'读取任务进度中'
  return <div className="portrait-online" aria-label="线上项目与作品交付">
    <p><strong>连接现有线上案板账号</strong> · 保存和生成会写入真实账号。</p>
    <p>{account?`当前账号：${account.name}`:'请先登录你的案板账号。'} <a href="/login" target="_blank" rel="noreferrer">登录线上账号</a> <button disabled={checking||busy} onClick={()=>void connect()}>{checking?'正在连接…':'检查连接'}</button></p>
    {error&&<p className="portrait-error" role="alert">{error}</p>}
    {receipt?.portraitKey!==key&&receipt&&<p role="alert">这次画像与已保存记录不同。请在项目页核对修改后再创作，避免覆盖或重复建项目。</p>}
    {receipt?.projectId&&<p>项目{saved?'及画像已保存':'已创建，画像尚待保存'}：<a target="_blank" rel="noreferrer" href={`/projects?edit=${encodeURIComponent(receipt.projectId)}`}>查看线上项目</a></p>}
    {saved&&type==='wechat-article'&&<p>公众号创作需要先绑定公众号，用于读取历史文章。<a href="/projects" target="_blank" rel="noreferrer">打开项目列表</a>，在本项目的「渠道配置」中设置。密钥仅在现有配置页填写。</p>}
    {!saved&&<button className="portrait-primary" disabled={!account||!receipt||busy||receipt.phase==='creating'||receipt.portraitKey!==key} onClick={()=>void save()}>{busy?'正在保存…':receipt?.projectId?'继续保存画像':'保存项目和已确认画像'}</button>}
    {receipt?.phase==='creating'&&!busy&&<p>项目创建结果需要核对，已暂停重复提交。<a href="/projects" target="_blank" rel="noreferrer">打开项目列表</a></p>}
    {saved&&!receipt?.taskId&&<>
      <p><label>创作形式 <select aria-label="创作形式" value={type} disabled={busy||receipt?.phase==='task_creating'} onChange={e=>setType(e.target.value as typeof type)}><option value="wechat-article">微信公众号文章</option><option value="seednote">小红书图文</option></select></label></p>
      <p><label>创作档位 <select aria-label="创作档位" value={profile} disabled={busy||receipt?.phase==='task_creating'} onChange={e=>setProfile(e.target.value as AgentExecutionProfileID)}><option value="">请选择可用档位</option>{account?.profiles.filter(p=>p.available).map(p=><option key={p.id} value={p.id}>{p.display_name} · {taskCostFor(account.catalog,type,p.id) ?? '费用未就绪'} 积分起</option>)}</select></label></p>
      {type==='wechat-article'&&<p><label><input type="checkbox" checked={images} disabled={busy||receipt?.phase==='task_creating'} onChange={e=>setImages(e.target.checked)}/> 同时生成封面和正文配图</label></p>}
      <p>任务启动费用：{cost??'暂不可用'} 积分；账号余额：{account?.wallet.balance}。图片等后续操作按线上实际用量计费。</p>
      <button className="portrait-primary" disabled={busy||receipt?.phase!=='saved'||receipt?.portraitKey!==key||!candidate.creationIdea||cost===undefined||cost>(account?.wallet.balance??0)} onClick={()=>void generate()}>生成第一篇作品</button>
      {receipt?.phase==='task_creating'&&!busy&&<p>提交结果需要核对，暂不重复生成。<a href="/tasks" target="_blank" rel="noreferrer">打开任务列表</a></p>}
    </>}
    {receipt?.taskId&&<p role="status">{status}。<a className="portrait-primary" href={`/tasks/${encodeURIComponent(receipt.taskId)}`} target="_blank" rel="noreferrer">查看进度与作品</a></p>}
    {task?.status==='failed'&&<p className="portrait-error">任务未成功交付，请在任务详情查看失败原因。这里不会自动重试扣费。</p>}
    {task?.status==='completed'&&task.outcome?.core_delivery.status==='none'&&<p className="portrait-error">任务已结束，但没有可交付正文，请查看详情。</p>}
  </div>
}
