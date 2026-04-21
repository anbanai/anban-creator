import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardBody } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import PageHeader from '@/components/layout/PageHeader'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { CreateAPIKeyResponse } from '@/types'

export default function SettingsPage() {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [keyName, setKeyName] = useState('')
  const [newKeyData, setNewKeyData] = useState<CreateAPIKeyResponse | null>(null)
  const [copied, setCopied] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null)

  const { data: apiKeys = [], isLoading } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
  })

  const createMutation = useMutation({
    mutationFn: (name: string) => api.apiKeys.create(name || 'Default'),
    onSuccess: (result) => {
      toast.success('密钥创建成功')
      setNewKeyData(result)
      setKeyName('')
      setShowCreate(false)
      queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.all })
    },
    onError: () => {
      toast.error('创建密钥失败，请重试')
    },
  })

  const revokeMutation = useMutation({
    mutationFn: (id: string) => api.apiKeys.revoke(id),
    onSuccess: () => {
      toast.success('密钥已吊销')
      queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.all })
      setRevokeTarget(null)
    },
    onError: () => {
      toast.error('吊销密钥失败，请重试')
    },
  })

  const handleCreate = () => {
    createMutation.mutate(keyName)
  }

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="space-y-6">
      <PageHeader title="设置" description="管理你的账号和偏好设置。" />

      {/* Profile Card */}
      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">个人资料</h2>
        </div>
        <CardBody className="space-y-3">
          <div>
            <p className="text-xs text-muted-foreground">邮箱</p>
            <p className="text-sm text-foreground">{user?.email || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">昵称</p>
            <p className="text-sm text-foreground">{user?.nickname || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">注册时间</p>
            <p className="text-sm text-foreground">
              {user?.created_at
                ? new Date(user.created_at).toLocaleDateString('zh-CN', {
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric',
                  })
                : '--'}
            </p>
          </div>
        </CardBody>
      </Card>

      {/* API Keys Card */}
      <Card>
        <div className="border-b border-border px-4 py-3 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-semibold text-foreground">平台密钥</h2>
            <p className="text-xs text-muted-foreground mt-0.5">用于 Claude Code 插件或第三方工具访问你的账号。</p>
            <p className="text-xs text-muted-foreground mt-1">
              不知道如何使用密钥？<Link to="/connect" className="text-primary hover:underline">查看接入指南 →</Link>
            </p>
          </div>
          <Button size="sm" onClick={() => setShowCreate(true)} disabled={showCreate}>
            创建密钥
          </Button>
        </div>
        <CardBody className="space-y-3">
          {/* New key display */}
          {newKeyData && (
            <div className="rounded-lg border border-green-500/30 bg-green-500/5 p-3 space-y-2">
              <p className="text-xs font-medium text-green-600">密钥创建成功！请立即复制，此密钥只显示一次。</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded bg-muted px-2 py-1.5 text-xs font-mono break-all select-all">
                  {newKeyData.key}
                </code>
                <Button size="sm" variant="outline" onClick={() => handleCopy(newKeyData.key)}>
                  {copied ? '已复制' : '复制'}
                </Button>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setNewKeyData(null)}>
                我已保存密钥
              </Button>
            </div>
          )}

          {/* Create form */}
          {showCreate && (
            <div className="flex items-center gap-2">
              <Input
                placeholder="密钥名称（如：我的 Mac）"
                value={keyName}
                onChange={(e) => setKeyName(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
                className="flex-1"
              />
              <Button size="sm" onClick={handleCreate} disabled={createMutation.isPending}>
                {createMutation.isPending ? '创建中...' : '确定'}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => { setShowCreate(false); setKeyName('') }}>
                取消
              </Button>
            </div>
          )}

          {/* Key list */}
          {isLoading ? (
            <p className="text-xs text-muted-foreground">加载中...</p>
          ) : apiKeys.length === 0 ? (
            <p className="text-xs text-muted-foreground">暂无密钥。创建一个密钥以在 Claude Code 中使用。</p>
          ) : (
            <div className="space-y-2">
              {apiKeys.map((key) => (
                <div
                  key={key.id}
                  className="flex items-center justify-between rounded-lg border border-border px-3 py-2"
                >
                  <div className="space-y-0.5">
                    <p className="text-sm font-medium text-foreground">{key.name || '未命名'}</p>
                    <p className="text-xs text-muted-foreground font-mono">{key.key_prefix}{'*'.repeat(20)}</p>
                    <p className="text-xs text-muted-foreground">
                      创建于 {new Date(key.created_at).toLocaleDateString('zh-CN')}
                      {key.last_used_at && (
                        <> · 最后使用 {new Date(key.last_used_at).toLocaleDateString('zh-CN')}</>
                      )}
                    </p>
                  </div>
                  <Button size="sm" variant="ghost" className="text-red-500 hover:text-red-600" onClick={() => setRevokeTarget(key.id)}>
                    吊销
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardBody>
      </Card>

      {/* Revoke confirmation */}
      <AlertDialog open={!!revokeTarget} onOpenChange={(v) => { if (!v) setRevokeTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定要吊销此密钥吗？</AlertDialogTitle>
            <AlertDialogDescription>吊销后使用此密钥的应用将无法继续访问，此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => { if (revokeTarget) revokeMutation.mutate(revokeTarget) }}>
              吊销
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
