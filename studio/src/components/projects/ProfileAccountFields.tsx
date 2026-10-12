import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { ProjectProfileSetup } from '@/types'

type Account = ProjectProfileSetup['platform_accounts'][number]
const platforms: Array<[Account['platform'], string]> = [
  ['wechat', '公众号'], ['seednote', '种草笔记'], ['douyin', '抖音'], ['bilibili', '哔哩哔哩'],
  ['weibo', '微博'], ['zhihu', '知乎'], ['kuaishou', '快手'], ['channels', '视频号'],
]

export function ProfileAccountFields({ accounts, onChange }: { accounts: Account[]; onChange: (accounts: Account[]) => void }) {
  function change(index: number, next: Partial<Account>) {
    onChange(accounts.map((account, i) => i === index ? { ...account, ...next } : account))
  }
  return <section aria-label="平台账号资料" className="space-y-3 sm:col-span-2">
    <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-sm font-medium">平台账号与公开主页</p><Button type="button" variant="outline" size="sm" disabled={accounts.length >= 10} onClick={() => onChange([...accounts, { platform: 'wechat', profile_url: '' }])}><Plus />添加账号</Button></div>
    <p className="text-xs text-muted-foreground">账号可选填；同一平台可记录多个账号，所有资料仅用于当前项目。</p>
    {accounts.map((account, index) => {
      const label = platforms.find(([value]) => value === account.platform)?.[1] ?? account.platform
      const duplicate = accounts.filter((entry) => entry.platform === account.platform).length > 1
      const suffix = duplicate ? ` · ${index + 1}` : ''
      const nameLabel = account.platform === 'seednote' ? '种草账号名称' : `${label}名称`
      const followerLabel = account.platform === 'seednote' ? '种草账号粉丝区间' : `${label}粉丝区间`
      return <fieldset key={index} className="grid gap-3 border border-border p-3 sm:grid-cols-2">
        <legend className="px-1 text-xs text-muted-foreground">账号 {index + 1}</legend>
        <label className="space-y-1.5 text-sm"><span>平台</span><select aria-label={`账号 ${index + 1} 平台`} value={account.platform} onChange={(event) => change(index, { platform: event.target.value as Account['platform'] })} className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm">{platforms.map(([value, name]) => <option key={value} value={value}>{name}</option>)}</select></label>
        <div className="flex items-center justify-between gap-2"><span className="border border-border px-1.5 py-1 font-mono text-[10px]">{account.profile_url.trim() ? '[用户确认]' : '[待补充]'}</span><Button type="button" variant="ghost" size="icon-md" onClick={() => onChange(accounts.filter((_, i) => i !== index))} aria-label={`删除账号 ${index + 1}`} title={`删除账号 ${index + 1}`}><Trash2 className="size-4" /></Button></div>
        <label className="space-y-1.5 text-sm sm:col-span-2"><span>主页链接 · {label}{suffix}</span><Input type="url" aria-label={`主页链接 · ${label}${suffix}`} value={account.profile_url} onChange={(event) => change(index, { profile_url: event.target.value })} placeholder="https://…" maxLength={500} /></label>
        <label className="space-y-1.5 text-sm"><span>{nameLabel}</span><Input aria-label={`${nameLabel}${suffix}`} value={account.account_name ?? ''} onChange={(event) => change(index, { account_name: event.target.value })} placeholder="选填" maxLength={100} /></label>
        <label className="space-y-1.5 text-sm"><span>{followerLabel}</span><Input aria-label={`${followerLabel}${suffix}`} value={account.follower_range ?? ''} onChange={(event) => change(index, { follower_range: event.target.value })} placeholder="例如：1万–5万" maxLength={50} /></label>
      </fieldset>
    })}
  </section>
}
