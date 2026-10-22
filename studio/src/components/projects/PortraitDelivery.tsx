import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { portraitKey, savePortraitProject, type PortraitReceipt } from '@/lib/portrait-online-save'
import type { PortraitCandidate, PortraitChatMessage } from '@/lib/portrait-chat-contract'

type Account = { id:string; name:string }
const receiptStorage = (userId:string) => `anban:portrait-online:${userId}`

/** Uses the existing authenticated Server APIs; never a local replacement project store. */
export default function PortraitDelivery({ candidate, messages, sessionId, production = false, initialIdea, onIdeaChange }: {candidate:PortraitCandidate;messages:PortraitChatMessage[];sessionId?:string;production?:boolean;initialIdea?:string;onIdeaChange?:(text:string)=>void}) {
  const navigate = useNavigate()
  const storageKey = (userId:string) => sessionId ? `anban:portrait-delivery:${userId}:${sessionId}` : receiptStorage(userId)
  const [account,setAccount] = useState<Account|null>(null)
  const [receipt,setReceipt] = useState<PortraitReceipt|null>(null)
  const [checking,setChecking] = useState(false)
  const [busy,setBusy] = useState(false)
  const [error,setError] = useState('')
  const [idea,setIdea] = useState(initialIdea ?? candidate.creationIdea?.text ?? '')
  const locked = useRef(false)
  const mounted = useRef(true)
  const key = portraitKey(candidate)
  function checkpoint(next:PortraitReceipt) {
    // This is only a recovery receipt. Project/profile/task truth stays on Server.
    sessionStorage.setItem(storageKey(next.userId), JSON.stringify(next))
    if (mounted.current) setReceipt(next)
  }
  async function connect() {
    if (locked.current) return
    setChecking(true); setError('')
    try {
      const user = await api.auth.me()
      if (!mounted.current) return
      setAccount({id:user.id,name:user.nickname || '当前账号'})
      const raw = sessionStorage.getItem(storageKey(user.id))
      const saved = raw ? JSON.parse(raw) as PortraitReceipt : null
      if (saved && (saved.userId !== user.id || !['new','creating','project_created','saving','saved','task_creating','task_created'].includes(saved.phase))) throw new Error('本机恢复记录无效，请先核对线上人设。')
      setReceipt(saved ?? {userId:user.id,portraitKey:key,phase:'new'})
    } catch(problem) { if (mounted.current) setError(getApiErrorMessage(problem,'无法读取线上账号，请先登录后重新检查连接。')) }
    finally { if (mounted.current) setChecking(false) }
  }
  useEffect(()=>{ mounted.current=true; void connect(); return()=>{mounted.current=false} },[])
  async function save() {
    if(locked.current||!account||!receipt||receipt.phase==='task_creating') return
    locked.current=true;setBusy(true);setError('')
    try {
      const user=await api.auth.me()
      if(user.id!==account.id) throw new Error('登录账号发生变化，请重新检查连接。')
      await savePortraitProject(api.projects,candidate,messages,receipt,checkpoint)
    } catch(problem) { if(mounted.current) setError(getApiErrorMessage(problem,'保存未完成，已取得的人设编号会保留，请核对后继续。')) }
    finally {locked.current=false;if(mounted.current)setBusy(false)}
  }
  function enterCreationMode() {
    if (!receipt?.projectId || receipt.portraitKey !== key || !['saved', 'task_created'].includes(receipt.phase)) return
    navigate('/', { state: { modeId: 'creation', projectId: receipt.projectId, initialMessage: idea.trim() } })
  }
  const saved=receipt?.portraitKey===key && receipt?.profileVersion!==undefined && ['saved','task_created'].includes(receipt.phase)
  const canSave = receipt && receipt.phase !== 'task_creating' && (receipt.portraitKey === key || receipt.phase === 'new' || ['saved','task_created'].includes(receipt.phase))
  return <div className="portrait-online" aria-label="线上人设与作品交付">
    <p><strong>{production ? '保存你的画像，进入创作模式' : '连接现有线上Anban账号'}</strong></p>
    <p>{account?`当前账号：${account.name}`:'请先登录你的Anban账号。'} {!production && <a href="/login" target="_blank" rel="noreferrer">登录线上账号</a>} <button disabled={checking||busy} onClick={()=>void connect()}>{checking?'正在连接…':'检查连接'}</button></p>
    {error&&<p className="portrait-error" role="alert">{error}</p>}
    {receipt?.portraitKey!==key&&receipt&&<p role="status">画像有了新的补充。保存后会更新同一个人设；若线上已有其他修改，会提示你核对。</p>}
    {receipt?.projectId&&<p>人设{saved?'及画像已保存':'已创建，画像尚待保存'}：<a target="_blank" rel="noreferrer" href={`/projects?edit=${encodeURIComponent(receipt.projectId)}`}>{production ? '查看人设' : '查看线上人设'}</a></p>}
    {!receipt?.taskId && <p><label>这次想创作什么？<textarea aria-label="这次想创作什么" value={idea} maxLength={1800} rows={3} disabled={busy} onChange={event=>{setIdea(event.target.value);onIdeaChange?.(event.target.value)}} placeholder="例如：写一篇给新手看的养花指南，也可以先描述一个大致想法。" /></label></p>}
    {!saved&&<button className="portrait-primary" disabled={!account||!receipt||busy||receipt.phase==='creating'||!canSave} onClick={()=>void save()}>{busy?'正在保存…':receipt?.projectId?'保存更新后的画像':'保存当前画像并继续'}</button>}
    {receipt?.phase==='creating'&&!busy&&<p>人设创建结果需要核对，已暂停重复提交。<a href="/projects" target="_blank" rel="noreferrer">打开人设列表</a></p>}
    {receipt?.phase==='task_creating'&&<p role="alert">旧记录中的作品任务提交结果未确认。请到任务列表核对，确认前不会覆盖画像记录或重新创建任务。<a href="/tasks" target="_blank" rel="noreferrer">打开任务列表</a></p>}
    {saved&&!receipt?.taskId&&<>
      <p>画像已经保存。接下来进入创作模式，由你确认创作计划后再创建作品任务。</p>
      <button className="portrait-primary" disabled={busy} onClick={enterCreationMode}>进入创作模式</button>
      {!idea.trim() && <p>可以先写下这次想创作什么，也可以进入创作模式后再一起确定选题。</p>}
    </>}
    {receipt?.taskId&&<p role="status">已有作品任务记录：<a className="portrait-primary" href={`/tasks/${encodeURIComponent(receipt.taskId)}`} target="_blank" rel="noreferrer">查看进度与作品</a></p>}
  </div>
}
