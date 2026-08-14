import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleCheck, CircleX, Loader2, LogOut, QrCode, RefreshCw, Server } from 'lucide-react'
import { toast } from 'sonner'

import PageHeader from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'

export default function SeednoteAdminPage() {
  const queryClient = useQueryClient()
  const [qrCode, setQrCode] = useState<string | null>(null)
  const [showLogoutDialog, setShowLogoutDialog] = useState(false)

  const statusQuery = useQuery({
    queryKey: queryKeys.seednoteAdmin.loginStatus,
    queryFn: api.seednoteAdmin.loginStatus,
    refetchInterval: qrCode ? 2_000 : false,
  })

  const qrMutation = useMutation({
    mutationFn: api.seednoteAdmin.loginQRCode,
    onSuccess: ({ qrcode_image }) => {
      setQrCode(qrcode_image)
      void statusQuery.refetch()
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '获取登录二维码失败')),
  })

  const logoutMutation = useMutation({
    mutationFn: api.seednoteAdmin.logout,
    onSuccess: async () => {
      setQrCode(null)
      setShowLogoutDialog(false)
      await queryClient.invalidateQueries({ queryKey: queryKeys.seednoteAdmin.loginStatus })
      toast.success('已退出小红书登录')
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '退出小红书登录失败')),
  })

  useEffect(() => {
    if (qrCode && statusQuery.data?.logged_in) {
      setQrCode(null)
      toast.success('小红书登录成功')
    }
  }, [qrCode, statusQuery.data?.logged_in])

  const status = statusQuery.data
  const statusIcon = status?.logged_in
    ? <CircleCheck className="size-5 text-emerald-600" />
    : <CircleX className="size-5 text-amber-600" />

  return (
    <div className="space-y-6">
      <PageHeader title="小红书账号" description="管理小红书数据研究所使用的后台会话。">
        <Button
          variant="outline"
          onClick={() => void statusQuery.refetch()}
          disabled={statusQuery.isFetching}
        >
          {statusQuery.isFetching ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          刷新
        </Button>
      </PageHeader>

      {statusQuery.isError && (
        <Alert variant="destructive">
          <CircleX />
          <AlertTitle>状态获取失败</AlertTitle>
          <AlertDescription>{getApiErrorMessage(statusQuery.error, '暂时无法获取小红书登录状态')}</AlertDescription>
        </Alert>
      )}

      <Card className="max-w-3xl">
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            <Server className="size-4" />
            账号会话
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">服务状态</div>
              <div className="flex items-center gap-2 font-medium">
                {status?.available ? <CircleCheck className="size-5 text-emerald-600" /> : <CircleX className="size-5 text-destructive" />}
                {statusQuery.isLoading ? '检查中' : status?.available ? '可用' : '不可用'}
              </div>
            </div>
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">登录状态</div>
              <div className="flex items-center gap-2 font-medium">
                {statusIcon}
                {statusQuery.isLoading ? '检查中' : status?.logged_in ? '已登录' : '未登录'}
                {status?.logged_in && <Badge variant="secondary">研究能力已就绪</Badge>}
              </div>
            </div>
          </div>

          {status?.message && (
            <div className="border-l-2 border-muted-foreground/30 pl-3 text-sm text-muted-foreground">
              {status.message}
            </div>
          )}

          {qrCode && !status?.logged_in && (
            <div className="flex flex-col gap-3 border-t pt-5 sm:flex-row sm:items-center">
              <img
                src={`data:image/png;base64,${qrCode}`}
                alt="小红书登录二维码"
                className="aspect-square size-56 shrink-0 border bg-white object-contain p-2"
              />
              <div className="space-y-2">
                <div className="font-medium">登录二维码</div>
                <div className="text-sm text-muted-foreground">页面正在等待登录状态更新。</div>
              </div>
            </div>
          )}

          <div className="flex flex-wrap gap-2 border-t pt-5">
            {!status?.logged_in && (
              <Button
                onClick={() => qrMutation.mutate()}
                disabled={!status?.available || qrMutation.isPending}
              >
                {qrMutation.isPending ? <Loader2 className="animate-spin" /> : <QrCode />}
                获取登录二维码
              </Button>
            )}
            {status?.logged_in && (
              <Button variant="destructive" onClick={() => setShowLogoutDialog(true)}>
                <LogOut />
                退出登录
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      <AlertDialog open={showLogoutDialog} onOpenChange={setShowLogoutDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定退出小红书登录？</AlertDialogTitle>
            <AlertDialogDescription>退出后，小红书研究和数据采集会暂停，直到管理员重新登录。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={logoutMutation.isPending}
              onClick={() => logoutMutation.mutate()}
            >
              {logoutMutation.isPending && <Loader2 className="animate-spin" />}
              退出登录
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
