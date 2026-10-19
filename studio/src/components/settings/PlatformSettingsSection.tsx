import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/common/button'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/PasswordInput'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import type { Project, ProjectProfileSetup } from '@/types'

type PlatformAccount = { platform: string; profile_url: string; account_name?: string }

const platformLabels: Record<string, string> = {
  wechat: '公众号', seednote: '小红书', douyin: '抖音', bilibili: '哔哩哔哩',
  weibo: '微博', zhihu: '知乎', kuaishou: '快手', channels: '视频号',
}

function accountsFor(project: Project): PlatformAccount[] {
  const setup = project.profile_setup as ProjectProfileSetup | undefined
  const accounts = setup?.platform_accounts ?? []
  if (accounts.length) return accounts
  return project.platform ? [{ platform: project.platform, profile_url: project.profile_url ?? '', account_name: project.name }] : []
}

export default function PlatformSettingsSection() {
  const queryClient = useQueryClient()
  const [projectID, setProjectID] = useState('')
  const [platform, setPlatform] = useState('')
  const [appID, setAppID] = useState('')
  const [secret, setSecret] = useState('')
  const [writer, setWriter] = useState('')
  const [theme, setTheme] = useState('')
  const [author, setAuthor] = useState('')
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects.list({ status: 'active' }),
    queryFn: () => api.projects.list({ status: 'active' }),
  })
  const projects = projectsQuery.data ?? []
  const project = projects.find((item) => item.id === projectID) ?? projects[0]
  const accounts = useMemo(() => project ? accountsFor(project) : [], [project])
  const activeAccount = accounts.find((account) => account.platform === platform) ?? accounts[0]

  useEffect(() => {
    if (project && projectID !== project.id) setProjectID(project.id)
  }, [project, projectID])
  useEffect(() => {
    if (activeAccount && platform !== activeAccount.platform) setPlatform(activeAccount.platform)
  }, [activeAccount, platform])
  useEffect(() => {
    if (!project) return
    setWriter(project.writer ?? '')
    setTheme(project.theme ?? '')
    setAuthor(project.author ?? '')
  }, [project?.id, project?.writer, project?.theme, project?.author])

  const wechatQuery = useQuery({
    queryKey: ['project-wechat-platform-config', project?.id],
    queryFn: () => api.projects.getWechatPlatformConfig(project!.id),
    enabled: Boolean(project && platform === 'wechat'),
  })
  useEffect(() => {
    if (wechatQuery.data) setAppID(wechatQuery.data.app_id ?? '')
  }, [wechatQuery.data])

  const appIDChanged = wechatQuery.data ? appID.trim() !== wechatQuery.data.app_id.trim() : false
  const requiresSecret = !wechatQuery.data?.secret_configured || Boolean(wechatQuery.data?.credentials_conflict) || appIDChanged
  const canSaveCredentials = Boolean(appID.trim()) && (!requiresSecret || Boolean(secret.trim()))
  const saveCredentials = useMutation({
    mutationFn: () => api.projects.upsertWechatPlatformConfig(project!.id, {
      app_id: appID.trim(),
      ...(secret.trim() ? { secret: secret.trim() } : {}),
    }),
    onSuccess: async () => {
      setSecret('')
      toast.success('公众号凭据已保存')
      await queryClient.invalidateQueries({ queryKey: ['project-wechat-platform-config', project?.id] })
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '公众号凭据保存失败')),
  })
  const saveDefaults = useMutation({
    mutationFn: () => api.projects.update(project!.id, { writer, theme, author }),
    onSuccess: async () => {
      toast.success('公众号创作默认值已保存')
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects.all })
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '公众号创作默认值保存失败')),
  })

  if (projectsQuery.isLoading) return <Card><CardContent className="py-8 text-sm text-muted-foreground">正在加载人设和平台账号…</CardContent></Card>
  if (projectsQuery.isError) return <Card><CardContent className="flex items-center gap-3 py-6 text-sm"><span>平台账号加载失败</span><Button size="sm" variant="outline" onClick={() => void projectsQuery.refetch()}>重试</Button></CardContent></Card>
  if (!projects.length) return <Card><CardContent className="py-6 text-sm text-muted-foreground">还没有人设。创建人设并关联平台账号后，可在这里管理平台设置。</CardContent></Card>

  return (
    <div className="space-y-4" data-testid="platform-settings">
      <Card><CardContent className="flex flex-wrap items-end gap-4 py-4">
        <label className="grid min-w-56 flex-1 gap-1.5 text-sm font-medium">人设
          <select aria-label="选择人设" value={project?.id ?? ''} onChange={(event) => { setProjectID(event.target.value); setPlatform('') }} className="h-9 rounded-md border border-input bg-background px-3 text-sm font-normal">
            {projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </label>
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">同一人设可关联多个平台账号，选择账号查看对应设置。</div>
      </CardContent></Card>

      {!accounts.length ? <Card><CardContent className="py-6 text-sm text-muted-foreground">这个人设还没有关联平台账号。请先在人设资料中补充账号。</CardContent></Card> : <Tabs value={platform || activeAccount?.platform} onValueChange={setPlatform}>
        <TabsList variant="line" aria-label="平台账号" className="h-auto w-full flex-wrap justify-start rounded-none border-b border-border p-0">
          {accounts.map((account, index) => <TabsTrigger key={`${account.platform}-${index}`} value={account.platform} className="flex-none rounded-none px-3 py-2">
            {platformLabels[account.platform] ?? account.platform}{account.account_name ? ` · ${account.account_name}` : ''}
          </TabsTrigger>)}
        </TabsList>
        <TabsContent value="wechat">
          {activeAccount?.platform === 'wechat' ? <Card>
          <div className="border-b border-border px-4 py-3"><h3 className="text-sm font-semibold">公众号设置</h3><p className="mt-1 text-xs text-muted-foreground">{activeAccount.account_name || project?.name}{activeAccount.profile_url ? ` · ${activeAccount.profile_url}` : ''}</p></div>
          <CardContent className="space-y-5 py-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="grid gap-1.5 text-sm font-medium">AppID<Input aria-label="微信 AppID" value={appID} onChange={(event) => setAppID(event.target.value)} placeholder="公众号 AppID" /></label>
              <label className="grid gap-1.5 text-sm font-medium">AppSecret<PasswordInput aria-label="微信 AppSecret" autoComplete="new-password" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder={wechatQuery.data?.secret_configured ? '已配置，留空表示不修改' : '公众号 AppSecret'} /></label>
            </div>
            {wechatQuery.data?.credentials_conflict && <p role="alert" className="text-sm text-destructive">公众号凭据存在冲突，请重新填写 AppSecret 后统一账号。</p>}
            <div className="flex items-center justify-between gap-3 border-t border-border pt-4">
              <span className="text-xs text-muted-foreground">{wechatQuery.data?.secret_configured ? 'AppSecret 已保存，不会回显' : '尚未配置 AppSecret'}</span>
              <Button onClick={() => saveCredentials.mutate()} disabled={saveCredentials.isPending || wechatQuery.isLoading || !canSaveCredentials}>{saveCredentials.isPending ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}保存公众号凭据</Button>
            </div>
            <div className="grid gap-4 border-t border-border pt-4 sm:grid-cols-2">
              <PersonaBlock writer={writer} onWriter={setWriter} author={author} onAuthor={setAuthor} />
              <ThemePicker theme={theme} onTheme={setTheme} />
            </div>
            <div className="flex justify-end border-t border-border pt-4">
              <Button onClick={() => saveDefaults.mutate()} disabled={saveDefaults.isPending}>{saveDefaults.isPending ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}保存创作默认值</Button>
            </div>
          </CardContent>
        </Card> : null}
        </TabsContent>
        {accounts.filter((account) => account.platform !== 'wechat').map((account, index) => <TabsContent key={`${account.platform}-${index}`} value={account.platform}>
          {account.platform === 'seednote' ? <Card><CardContent className="py-6"><h3 className="text-sm font-semibold">小红书设置</h3><p className="mt-2 text-sm text-muted-foreground">账号相关配置在作品创建流程中处理。</p></CardContent></Card> : <Card><CardContent className="py-6"><h3 className="text-sm font-semibold">{platformLabels[account.platform] ?? account.platform}设置</h3><p className="mt-2 text-sm text-muted-foreground">该平台的专属设置尚未接入。账号信息已在当前人设中展示。</p></CardContent></Card>}
        </TabsContent>)}
      </Tabs>}
    </div>
  )
}
