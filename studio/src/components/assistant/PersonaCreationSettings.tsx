import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Clapperboard, Layers3, Scissors } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { getApiErrorMessage } from '@/lib/http-client'
import { assistantPrimaryPlatform } from '@/lib/assistant-creation-types'
import { profileLabAgents, projectAgentIDs } from '@/lib/project-capabilities'
import { platformIcon, platformIconColor } from '@/lib/PlatformIcon'
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
    <PopoverContent align="end" side="bottom" sideOffset={8} className="max-h-[min(28rem,var(--available-height))] w-[min(17rem,calc(100vw-2rem))] gap-0 overflow-hidden rounded-xl p-0 shadow-lg">
      <div className="min-w-0 shrink-0 space-y-0.5 px-3.5 pb-2 pt-3">
        <div className="flex items-center justify-between gap-2">
          <PopoverTitle className="text-[13px] font-semibold">创作类型</PopoverTitle>
          <span className="text-[11px] text-muted-foreground">可多选</span>
        </div>
        <PopoverDescription className="truncate text-xs leading-5">{project.name}</PopoverDescription>
      </div>
      <div role="group" aria-label="创作类型" className="assistant-scroll min-h-0 space-y-0.5 overflow-y-auto overscroll-contain px-1.5 pb-1.5">
        {profileLabAgents.filter(agent => user?.is_admin || agent.id !== 'hypit').map(agent => {
          const selected = agents.includes(agent.id)
          const Icon = platformIcon[agent.id] ?? (agent.id === 'video-edit' ? Scissors : agent.id === 'local-life' ? Clapperboard : Layers3)
          return <button key={agent.id} type="button" aria-label={agent.label} aria-pressed={selected} disabled={saving} onClick={() => setAgents(current => selected ? current.filter(id => id !== agent.id) : [...current, agent.id])} className={`flex min-h-9 w-full min-w-0 items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-not-allowed disabled:opacity-50 pointer-coarse:min-h-11 ${selected ? 'bg-muted/60 hover:bg-muted' : 'hover:bg-muted/60'}`}>
            <span aria-hidden="true" className={platformIconColor[agent.id] ?? 'text-muted-foreground'}><Icon className="size-4 shrink-0" /></span>
            <span className="min-w-0 flex-1 text-[13px] leading-5">{agent.label}</span>
            <Check aria-hidden="true" className={`size-3.5 shrink-0 ${selected ? 'text-foreground' : 'invisible'}`} />
          </button>
        })}
      </div>
      <div className="shrink-0 space-y-2 border-t border-border/60 px-2.5 py-2">
        {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] text-muted-foreground">已选 {agents.length} 项</span>
          <div className="flex gap-1">
            <Button type="button" variant="ghost" size="sm" className="h-7 rounded-md px-2 text-xs" disabled={saving} onClick={() => setOpen(false)}>取消</Button>
            <Button type="button" size="sm" className="h-7 rounded-md px-2.5 text-xs" disabled={saving || !agents.length} onClick={() => void save()}>{saving ? '保存中…' : '保存设置'}</Button>
          </div>
        </div>
      </div>
    </PopoverContent>
  </Popover>
}
