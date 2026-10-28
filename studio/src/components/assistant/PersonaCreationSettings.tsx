import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { getApiErrorMessage } from '@/lib/http-client'
import { assistantPrimaryPlatform } from '@/lib/assistant-creation-types'
import { profileLabAgents, projectAgentIDs } from '@/lib/project-capabilities'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import type { Project } from '@/types'

export function PersonaCreationSettings({ project, disabled }: { project: Pick<Project, 'id' | 'name' | 'platform' | 'agent_ids'>; disabled: boolean }) {
  const { user } = useAuth()
  const cache = useQueryClient()
  const [open, setOpen] = useState(false)
  const [agents, setAgents] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save() {
    if (saving || !agents.length) return
    setSaving(true)
    setError('')
    try {
      const platform = assistantPrimaryPlatform(project.platform, agents)
      const updated = await api.projects.update(project.id, {
        agent_ids: agents,
        ...(platform && platform !== project.platform ? { platform } : {}),
      })
      cache.setQueryData<Project[]>(['projects', 'assistant-home'], current => current?.map(item => item.id === project.id ? updated : item))
      await Promise.all([
        cache.invalidateQueries({ queryKey: ['projects'] }),
        cache.invalidateQueries({ queryKey: queryKeys.projects.detail(project.id) }),
        cache.invalidateQueries({ queryKey: ['assistant-bootstrap'] }),
      ])
      setOpen(false)
    } catch (err) {
      setError(getApiErrorMessage(err, '保存失败，请重试。'))
    } finally {
      setSaving(false)
    }
  }

  return <Popover open={open} onOpenChange={(next) => {
    if (saving) return
    if (next) {
      setAgents(projectAgentIDs(project))
      setError('')
    }
    setOpen(next)
  }}>
    <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" disabled={disabled} aria-label={`创作类型：${project.name}`} className="h-7 px-1.5 text-[11px]" />}>创作类型</PopoverTrigger>
    <PopoverContent align="end" side="bottom" className="assistant-scroll max-h-[var(--available-height)] w-[min(22rem,calc(100vw-2rem))] gap-3 overflow-y-auto overscroll-contain p-3">
      <div className="min-w-0 space-y-1">
        <PopoverTitle>创作类型</PopoverTitle>
        <PopoverDescription className="text-xs">为「{project.name}」选择内容类型，可多选。</PopoverDescription>
      </div>
      <div role="group" aria-label="创作类型" className="grid grid-cols-2 gap-1.5">
        {profileLabAgents.filter(agent => user?.is_admin || agent.id !== 'hypit').map(agent => {
          const selected = agents.includes(agent.id)
          return <Button key={agent.id} type="button" variant="outline" size="sm" aria-label={agent.label} aria-pressed={selected} disabled={saving} onClick={() => setAgents(current => selected ? current.filter(id => id !== agent.id) : [...current, agent.id])} className={`h-10 min-w-0 justify-between px-2 text-xs ${selected ? 'border-primary/50 bg-primary/5' : ''}`}>
            <span className="truncate">{agent.label}</span><Check className={`size-3.5 shrink-0 ${selected ? 'text-primary' : 'invisible'}`} />
          </Button>
        })}
      </div>
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={() => setOpen(false)}>取消</Button>
        <Button type="button" size="sm" disabled={saving || !agents.length} onClick={() => void save()}>{saving ? '保存中…' : '保存并继续创作'}</Button>
      </div>
    </PopoverContent>
  </Popover>
}
