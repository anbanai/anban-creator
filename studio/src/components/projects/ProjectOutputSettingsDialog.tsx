import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAuth } from '@/contexts/AuthContext'
import { queryKeys } from '@/lib/query-keys'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { profileLabAgents, projectAgentIDs } from '@/lib/project-capabilities'
import { assistantPrimaryPlatform } from '@/lib/assistant-creation-types'
import { CreationTypePicker } from '@/components/tasks/CreationTypePicker'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import type { Project } from '@/types'

export function ProjectOutputSettingsDialog({ project, onClose, onSaved, syncAssistantPlatform = false }: { project: Project; onClose: () => void; onSaved: (project: Project) => void; syncAssistantPlatform?: boolean }) {
  const { user } = useAuth()
  const cache = useQueryClient()
  const [agents, setAgents] = useState(projectAgentIDs(project))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  async function save() {
    if (saving || !agents.length) return
    setSaving(true); setError('')
    try {
      const platform = syncAssistantPlatform ? assistantPrimaryPlatform(project.platform, agents) : undefined
      const result = await api.projects.update(project.id, { agent_ids: agents, ...(platform && platform !== project.platform ? { platform } : {}) })
      const updated = result
      cache.setQueryData<Project[]>(['projects', 'active'], current => current?.map(item => item.id === project.id ? updated : item))
      cache.setQueryData<Project[]>(['projects', 'assistant-home'], current => current?.map(item => item.id === project.id ? updated : item))
      await Promise.all([cache.invalidateQueries({ queryKey: ['projects'] }), cache.invalidateQueries({ queryKey: queryKeys.projects.detail(project.id) })])
      onSaved(updated)
    } catch (err) { setError(getApiErrorMessage(err, '保存失败，请重试。')) }
    finally { setSaving(false) }
  }
  return <Dialog open onOpenChange={next => { if (!next && !saving) onClose() }}>
    <DialogContent closeButtonDisabled={saving} className="sm:max-w-2xl">
      <DialogHeader><DialogTitle>设置创作类型</DialogTitle><DialogDescription>为「{project.name}」选择要创作的内容，可多选。保存后回到刚才的创作内容，不会立即生成作品。</DialogDescription></DialogHeader>
      {syncAssistantPlatform && <p className="text-xs text-muted-foreground">部分类型需进入创作页填写素材，保存后会提供入口。</p>}
      <CreationTypePicker multiple disabled={saving} options={profileLabAgents.filter(agent => user?.is_admin || agent.id !== 'hypit')} value={agents} onChange={setAgents} />
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <DialogFooter><Button variant="outline" disabled={saving} onClick={onClose}>取消</Button><Button disabled={saving || !agents.length} onClick={() => void save()}>{saving ? '保存中…' : '保存并继续创作'}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}
