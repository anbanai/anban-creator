import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, MessageCircle } from 'lucide-react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { normalizeProjectProfile, PROJECT_PROFILE_DIMENSIONS } from '@/lib/project-profile'
import { portraitLabels, portraitProgress, portraitStatus, updatePortraitHref } from '@/lib/portrait-library'
import { createTaskHref } from '@/lib/command-center'
import { profileLabAgents, projectAgentIDs, supportsProfileLab } from '@/lib/project-capabilities'
import { ProfileDimensionCard } from '@/components/projects/ProfileDimensionCard'
import { ProfileAccountFields } from '@/components/projects/ProfileAccountFields'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Project, ProjectProfileSetup } from '@/types'

type DimensionKey = typeof PROJECT_PROFILE_DIMENSIONS[number]
type Edit = { text: string; version: number }

export default function PortraitDetailPage() {
  const { projectId = '' } = useParams()
  return <PortraitDetail key={projectId} projectId={projectId} />
}

function PortraitDetail({ projectId }: { projectId: string }) {
  const cache = useQueryClient()
  const projectQuery = useQuery({ queryKey: queryKeys.projects.detail(projectId), queryFn: ({ signal }) => api.projects.get(projectId, signal) })
  const project = projectQuery.data?.project
  const query = useQuery({ queryKey: ['project-profile', projectId], queryFn: () => api.projects.getAccountProfile(projectId), enabled: Boolean(project && supportsProfileLab(project)) })
  const profile = normalizeProjectProfile(query.data)
  const [edits, setEdits] = useState<Partial<Record<DimensionKey, Edit>>>({})
  const [errors, setErrors] = useState<Partial<Record<DimensionKey, string>>>({})
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState('')
  const dirty = Object.keys(edits).length > 0
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])
  const busy = saving || query.isError || profile?.initialization_status === 'running' || profile?.initialization_status === 'queued'
  async function save(key: DimensionKey) {
    if (!profile || !edits[key] || busy) return
    const edit = edits[key]
    setSaving(true); setNotice('')
    try {
      const next = await api.projects.updateProfileDimension(projectId, key, edit.version, { ...profile.dimensions[key], content: JSON.parse(edit.text) })
      cache.setQueryData(['project-profile', projectId], next)
      // Our own successful save advances the revision for other local edits.
      // Never rebase edits across another writer's revision.
      setEdits(current => Object.fromEntries(Object.entries(current).filter(([name]) => name !== key).map(([name, value]) => [name, value.version === edit.version ? { ...value, version: next.version } : value])))
      setErrors(current => ({ ...current, [key]: '' }))
      setNotice(`${portraitLabels[key]}已保存`)
      void cache.invalidateQueries({ queryKey: ['projects'] })
      void cache.invalidateQueries({ queryKey: ['project-memory', projectId] })
      void cache.invalidateQueries({ queryKey: ['project-profile-revisions', projectId] })
    } catch (error) {
      setErrors(current => ({ ...current, [key]: getApiErrorMessage(error, '保存失败，你的修改仍保留在页面中。') }))
      void query.refetch()
    } finally { setSaving(false) }
  }
  if (projectQuery.isPending) return <p className="p-8" role="status">正在读取创作身份…</p>
  if (!project || projectQuery.isError) return <div className="space-y-3 p-8" role="alert"><p>无法读取这个创作身份，请确认账号和访问权限。</p><Button onClick={() => void projectQuery.refetch()}>重试</Button><Link to="/projects">返回人设</Link></div>
  if (!supportsProfileLab(project)) return <div className="p-8"><p>该人设使用专属创作设置。</p><Link to="/projects">前往人设管理</Link></div>
  const progress = portraitProgress(profile)
  return <section className="mx-auto max-w-5xl space-y-6 p-4 md:p-8">
    <Link className="inline-flex min-h-10 items-center gap-2 text-sm text-muted-foreground" to="/projects"><ArrowLeft className="size-4" />返回人设</Link>
    <header className="flex flex-wrap items-start justify-between gap-4"><div><p className="mb-2 text-sm text-muted-foreground">创作身份{project.status === 'archived' ? ' · 已归档' : ''}</p><h1 className="text-2xl font-semibold">{project.name}</h1><p className="mt-2 text-sm text-muted-foreground">同一份画像用于该身份的不同平台账号。修改后会用于后续创作，已有作品不会被改写。</p></div><div className="flex flex-wrap gap-2"><Link className="inline-flex min-h-11 items-center gap-2 rounded-md border px-4 text-sm" to={updatePortraitHref(projectId)}><MessageCircle className="size-4" />继续访谈</Link>{project.status !== 'archived' && <Link className="inline-flex min-h-11 items-center rounded-md bg-primary px-4 text-sm text-primary-foreground" to={createTaskHref({ projectId })}>用此画像创作</Link>}</div></header>
    {project.status === 'archived' && <p className="rounded-lg bg-muted p-3 text-sm">画像仍可查看和修改；如需继续创作，请先到人设页恢复该人设。<Link className="ml-2 underline" to="/projects">前往人设</Link></p>}
    {dirty && <p role="status" className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900">有未保存的修改，请保存对应资料后再离开或继续访谈。</p>}
    {notice && <p role="status" className="text-sm text-emerald-700">{notice}</p>}
    <section className="rounded-xl border bg-card p-5 md:p-6" aria-label="身份画像">
      <div className="mb-6 flex flex-wrap justify-between gap-2"><h2 className="font-semibold">身份画像</h2>{profile && <span className="text-sm text-muted-foreground">{portraitStatus(profile)} · 已填写 {progress.count}/6 项</span>}</div>
      {query.isPending ? <p role="status">正在读取画像…</p> : query.isError ? <div role="alert"><p>画像读取失败，请重试后再编辑。</p><Button onClick={() => void query.refetch()}>重试</Button></div> : profile && PROJECT_PROFILE_DIMENSIONS.map(key => {
        const edit = edits[key]
        const stale = edit && edit.version !== profile.version
        let content = profile.dimensions[key].content
        try { if (edit) content = JSON.parse(edit.text) } catch { /* Advanced editor keeps malformed text until corrected. */ }
        return <div key={key}>{stale && <p role="alert" className="text-sm text-amber-800">{portraitLabels[key]}的服务器版本已变化。你的修改仍在，请先复制保留，再载入最新内容。</p>}{stale && <Button variant="outline" size="sm" onClick={() => { setEdits(current => { const next = { ...current }; delete next[key]; return next }); setErrors(current => ({ ...current, [key]: '' })) }}>载入最新{portraitLabels[key]}</Button>}
          <ProfileDimensionCard label={portraitLabels[key]} dimension={{ ...profile.dimensions[key], content }} text={edit?.text ?? JSON.stringify(content, null, 2)} disabled={busy || Boolean(stale)} error={errors[key]} onTextChange={text => {
            setEdits(current => ({ ...current, [key]: { text, version: current[key]?.version ?? profile.version } }))
            let error = ''
            try { const value = JSON.parse(text); if (!value || typeof value !== 'object' || Array.isArray(value)) error = '内容须为有效对象' } catch { error = '高级编辑内容格式不正确，请检查后保存' }
            setErrors(current => ({ ...current, [key]: error }))
          }} onSave={() => void save(key)} />
        </div>
      })}
    </section>
    <IdentityAccounts project={project} />
  </section>
}

function IdentityAccounts({ project }: { project: Project }) {
  const cache = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(project.name)
  const [accounts, setAccounts] = useState(project.profile_setup?.platform_accounts ?? [])
  const [agents, setAgents] = useState(projectAgentIDs(project))
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  async function save() {
    if (!name.trim() || saving) return
    setSaving(true); setError(''); setMessage('')
    try {
      // Preserve all source answers/materials; linking accounts must not start a paid analysis.
      const setup: ProjectProfileSetup = { account_status: 'new', answers: { intent: { goals: '', direction: '', differentiation: '', strengths: '', resources: '' }, content: { preferences: '', formats: '', tone: '', audience: '', favorites: '', reference_accounts: '' }, boundaries: { exclusions: '', collaboration: '', compliance: '' } }, samples: [], ...project.profile_setup, platform_accounts: accounts, analyze: false }
      await api.projects.update(project.id, { name: name.trim(), agent_ids: agents, profile_setup: setup })
      await cache.invalidateQueries({ queryKey: ['projects'] })
      await cache.invalidateQueries({ queryKey: queryKeys.projects.detail(project.id) })
      setEditing(false); setMessage('身份和平台账号已保存，共用当前画像。')
    } catch (err) { setError(getApiErrorMessage(err, '保存失败，请重试。')) }
    finally { setSaving(false) }
  }
  return <section className="space-y-4 rounded-xl border bg-card p-5 md:p-6" aria-label="跨平台复用">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">平台账号与创作方式</h2><p className="mt-2 text-sm text-muted-foreground">将不同平台的账号放在同一创作身份下，直接共用这份画像，无需复制。账号资料不等于已授权发布。</p></div>{!editing && <Button variant="outline" onClick={() => { setName(project.name); setAccounts(project.profile_setup?.platform_accounts ?? []); setAgents(projectAgentIDs(project)); setEditing(true) }}>管理平台账号</Button>}</div>
    {message && <p role="status" className="text-sm text-emerald-700">{message}</p>}
    {!editing ? <p className="text-sm text-muted-foreground">已记录 {project.profile_setup?.platform_accounts?.length ?? 0} 个平台账号；同一个运营人员可以在人设中管理多个创作身份。</p> : <fieldset disabled={saving} className="space-y-5">
      <label className="block space-y-2 text-sm"><span>创作身份名称</span><Input value={name} maxLength={100} onChange={e => setName(e.target.value)} placeholder="例如：钢厂老板老王" /></label>
      <ProfileAccountFields accounts={accounts} onChange={setAccounts} />
      <div><p className="mb-2 text-sm font-medium">用于哪些创作方式</p><div className="flex flex-wrap gap-4">{profileLabAgents.map(agent => <label key={agent.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={agents.includes(agent.id)} onChange={e => setAgents(current => e.target.checked ? [...current, agent.id] : current.filter(id => id !== agent.id))} />{agent.label}</label>)}</div></div>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <div className="flex gap-2"><Button disabled={saving || !name.trim()} onClick={() => void save()}>{saving ? '保存中…' : '保存身份与账号'}</Button><Button variant="outline" onClick={() => setEditing(false)}>取消</Button></div>
    </fieldset>}
  </section>
}
