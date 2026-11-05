import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Check, CircleStop, Loader2, MessageCircle, RefreshCw, Send, Trash2, X } from 'lucide-react'
import { archestraApi } from '@/lib/api/archestra'
import { getApiErrorMessage } from '@/lib/http-client'
import type { ArchestraAgent, ArchestraApprovalRequest, ArchestraTask } from '@/types'

type ChatMessage = { id: string; role: 'user' | 'agent' | 'system'; text: string }

const ACTIVE_STATES = new Set(['TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING', 'TASK_STATE_AUTH_REQUIRED'])
const INPUT_REQUIRED = 'TASK_STATE_INPUT_REQUIRED'
const COMPLETED = 'TASK_STATE_COMPLETED'
const FAILED = 'TASK_STATE_FAILED'
const CANCELED = 'TASK_STATE_CANCELED'

function taskLabel(task: ArchestraTask | null): string {
  if (!task) return '准备就绪'
  if (task.state === INPUT_REQUIRED) return '等待审批'
  if (ACTIVE_STATES.has(task.state)) return '运行中'
  if (task.state === COMPLETED) return '已完成'
  if (task.state === CANCELED) return '已取消'
  if (task.state === FAILED) return '失败'
  return task.state.replace(/^TASK_STATE_/, '').toLowerCase()
}

export default function AdminArchestraPage() {
  const [agents, setAgents] = useState<ArchestraAgent[]>([])
  const [agentID, setAgentID] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [contextID, setContextID] = useState('')
  const [taskID, setTaskID] = useState('')
  const [task, setTask] = useState<ArchestraTask | null>(null)
  const [input, setInput] = useState('')
  const [loadingAgents, setLoadingAgents] = useState(true)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const pollTimer = useRef<number | undefined>(undefined)
  const sessionVersion = useRef(0)

  const selectedAgent = useMemo(() => agents.find(agent => agent.id === agentID), [agents, agentID])
  const approval = task?.approval_requests?.find(item => !item.resolved)
  const taskInProgress = Boolean(task && (ACTIVE_STATES.has(task.state) || task.state === INPUT_REQUIRED))
  const busy = sending || taskInProgress || Boolean(approval)

  const clearPoll = useCallback(() => {
    if (pollTimer.current !== undefined) window.clearTimeout(pollTimer.current)
    pollTimer.current = undefined
  }, [])

  useEffect(() => {
    let mounted = true
    setLoadingAgents(true)
    archestraApi.agents()
      .then(items => {
        if (!mounted) return
        setAgents(items)
        setAgentID(current => current || items[0]?.id || '')
        setError('')
      })
      .catch(err => mounted && setError(getApiErrorMessage(err, 'Archestra 尚未配置或暂时不可用')))
      .finally(() => mounted && setLoadingAgents(false))
    return () => { mounted = false; clearPoll() }
  }, [clearPoll])

  const applyTask = useCallback((next: ArchestraTask, version = sessionVersion.current) => {
    if (version !== sessionVersion.current) return
    setTask(next)
    if (next.context_id) setContextID(next.context_id)
    if (next.response) {
      setMessages(current => current.some(item => item.id === `task:${next.task_id}`)
        ? current
        : [...current, { id: `task:${next.task_id}`, role: 'agent', text: next.response! }])
    }
  }, [])

  const pollTask = useCallback((nextTaskID: string, nextAgentID: string, version = sessionVersion.current) => {
    clearPoll()
    const poll = async () => {
      try {
        const next = await archestraApi.task(nextTaskID, nextAgentID)
        if (version !== sessionVersion.current) return
        applyTask(next, version)
        if (ACTIVE_STATES.has(next.state)) {
          pollTimer.current = window.setTimeout(poll, 1000)
        }
      } catch (err) {
        setError(getApiErrorMessage(err, '读取 Archestra 任务状态失败'))
      }
    }
    void poll()
  }, [applyTask, clearPoll])

  const handleResult = useCallback((result: { message?: { text?: string; context_id?: string }; task?: ArchestraTask }, nextAgentID: string, version = sessionVersion.current) => {
    if (version !== sessionVersion.current) return
    if (result.message?.context_id) setContextID(result.message.context_id)
    if (result.message?.text) setMessages(current => [...current, { id: `agent:${Date.now()}`, role: 'agent', text: result.message!.text! }])
    if (result.task) {
      applyTask(result.task, version)
      setTaskID(result.task.task_id)
      if (ACTIVE_STATES.has(result.task.state)) pollTask(result.task.task_id, nextAgentID, version)
    }
  }, [applyTask, pollTask])

  const send = async (event?: FormEvent) => {
    event?.preventDefault()
    const text = input.trim()
    if (!text || !agentID || busy) return
    setInput('')
    setError('')
    setSending(true)
    const version = sessionVersion.current
    setMessages(current => [...current, { id: `user:${Date.now()}`, role: 'user', text }])
    try {
      const result = await archestraApi.sendMessage(agentID, { text, ...(contextID ? { context_id: contextID } : {}) })
      handleResult(result, agentID, version)
    } catch (err) {
      setError(getApiErrorMessage(err, '发送消息失败'))
    } finally {
      setSending(false)
    }
  }

  const decide = async (request: ArchestraApprovalRequest, approved: boolean) => {
    if (!taskID || !agentID) return
    setError('')
    setSending(true)
    const version = sessionVersion.current
    try {
      const result = await archestraApi.approval(taskID, { agent_id: agentID, context_id: contextID || undefined, approval_id: request.approval_id, approved })
      if (version !== sessionVersion.current) return
      setTask(current => current ? { ...current, approval_requests: current.approval_requests?.map(item => item.approval_id === request.approval_id ? { ...item, resolved: true } : item) } : current)
      handleResult(result, agentID, version)
    } catch (err) {
      setError(getApiErrorMessage(err, '提交审批决定失败'))
    } finally {
      setSending(false)
    }
  }

  const cancel = async () => {
    if (!taskID || !agentID) return
    clearPoll()
    setSending(true)
    const version = ++sessionVersion.current
    try {
      applyTask(await archestraApi.cancel(taskID, agentID), version)
    } catch (err) {
      setError(getApiErrorMessage(err, '取消任务失败'))
    } finally {
      setSending(false)
    }
  }

  const clearConversation = () => {
    sessionVersion.current += 1
    clearPoll()
    setMessages([])
    setContextID('')
    setTaskID('')
    setTask(null)
    setError('')
  }

  return (
    <div className="flex min-h-[calc(100dvh-2rem)] flex-col gap-5 p-5 lg:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4 border-b border-border pb-5">
        <div>
          <div className="flex items-center gap-2 text-sm font-medium text-primary"><MessageCircle className="size-4" />管理员工具</div>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight">Archestra Agent 测试</h1>
          <p className="mt-1 text-sm text-muted-foreground">通过 Anban Server 连接 Token 可访问的 Agent，当前对话只保存在本页。</p>
        </div>
        <div className="flex items-center gap-2">
          <select aria-label="选择 Agent" value={agentID} onChange={event => { setAgentID(event.target.value); clearConversation() }} disabled={loadingAgents || agents.length === 0 || sending} className="h-9 max-w-64 rounded-md border border-input bg-background px-3 text-sm">
            {agents.length === 0 ? <option value="">暂无可用 Agent</option> : agents.map(agent => <option key={agent.id} value={agent.id}>{agent.name || agent.id}</option>)}
          </select>
          <button type="button" onClick={clearConversation} disabled={sending} className="inline-flex h-9 items-center gap-2 rounded-md border border-input px-3 text-sm hover:bg-muted disabled:pointer-events-none disabled:opacity-50"><Trash2 className="size-4" />清空</button>
        </div>
      </header>

      {loadingAgents && <div role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取 Agent 列表…</div>}
      {error && <div role="alert" className="flex items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"><span>{error}</span><button type="button" aria-label="关闭错误" onClick={() => setError('')}><X className="size-4" /></button></div>}

      <main className="grid min-h-0 flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
        <section className="flex min-h-[520px] min-w-0 flex-col rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3"><div className="text-sm font-medium">{selectedAgent?.name || 'Archestra Chat'}</div><div className="text-xs text-muted-foreground">{taskLabel(task)}</div></div>
          <div className="flex-1 space-y-4 overflow-y-auto p-4">
            {messages.length === 0 && <div className="flex h-full min-h-64 items-center justify-center text-center text-sm text-muted-foreground"><div><MessageCircle className="mx-auto mb-3 size-8 opacity-50" /><p>选择 Agent 后发送第一条消息</p></div></div>}
            {messages.map(message => <div key={message.id} className={`flex ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}><div className={`max-w-[85%] whitespace-pre-wrap rounded-lg px-3 py-2 text-sm ${message.role === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground'}`}>{message.text}</div></div>)}
            {sending && <div className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />Agent 正在处理…</div>}
          </div>
          <form onSubmit={send} className="border-t border-border p-3"><div className="flex items-end gap-2"><textarea aria-label="消息" value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void send() } }} placeholder={agentID ? '输入消息，Enter 发送' : '请先选择 Agent'} disabled={!agentID || busy} rows={2} className="min-h-10 flex-1 resize-none rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring" /><button type="submit" disabled={!input.trim() || !agentID || busy} className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-3 text-sm text-primary-foreground disabled:pointer-events-none disabled:opacity-50"><Send className="size-4" />发送</button></div></form>
        </section>

        <aside className="space-y-4">
          <section className="rounded-lg border border-border bg-card p-4"><div className="flex items-center justify-between"><h2 className="text-sm font-medium">任务状态</h2>{task && (ACTIVE_STATES.has(task.state) || task.state === INPUT_REQUIRED) && <button type="button" onClick={() => void cancel()} disabled={sending} className="inline-flex items-center gap-1 rounded-md border border-input px-2 py-1 text-xs hover:bg-muted disabled:pointer-events-none disabled:opacity-50"><CircleStop className="size-3.5" />取消</button>}</div><dl className="mt-3 space-y-2 text-sm"><div className="flex justify-between gap-3"><dt className="text-muted-foreground">Task</dt><dd className="max-w-44 truncate font-mono text-xs">{taskID || '未创建'}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">Context</dt><dd className="max-w-44 truncate font-mono text-xs">{contextID || '未创建'}</dd></div><div className="flex justify-between gap-3"><dt className="text-muted-foreground">状态</dt><dd>{taskLabel(task)}</dd></div></dl>{task?.status_message && <p className="mt-3 border-t border-border pt-3 text-xs text-muted-foreground">{task.status_message}</p>}</section>
          {approval && <section className="rounded-lg border border-amber-300/60 bg-amber-50/60 p-4 dark:bg-amber-950/20"><h2 className="text-sm font-medium">需要管理员审批</h2><p className="mt-2 text-sm">{approval.text || 'Agent 请求执行一项需要确认的操作。'}</p>{approval.tool_name && <p className="mt-2 font-mono text-xs text-muted-foreground">{approval.tool_name}</p>}<div className="mt-4 flex gap-2"><button type="button" onClick={() => void decide(approval, true)} disabled={sending} className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"><Check className="size-4" />批准</button><button type="button" onClick={() => void decide(approval, false)} disabled={sending} className="inline-flex items-center gap-2 rounded-md border border-input px-3 py-2 text-sm hover:bg-background disabled:opacity-50"><X className="size-4" />拒绝</button></div></section>}
          {task?.state === COMPLETED && <section className="rounded-lg border border-emerald-300/60 bg-emerald-50/60 p-4 text-sm dark:bg-emerald-950/20">任务已完成，结果已显示在对话中。</section>}
          {task?.state === FAILED && <section className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">任务执行失败，请查看状态信息或重新发送。</section>}
          {task?.state === CANCELED && <section className="rounded-lg border border-border bg-muted/40 p-4 text-sm">任务已取消。</section>}
          {agents.length > 0 && <button type="button" onClick={() => { setLoadingAgents(true); archestraApi.agents().then(setAgents).catch(err => setError(getApiErrorMessage(err, '读取 Agent 列表失败'))).finally(() => setLoadingAgents(false)) }} className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><RefreshCw className="size-4" />刷新 Agent 列表</button>}
        </aside>
      </main>
    </div>
  )
}
