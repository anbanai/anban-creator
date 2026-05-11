import { useState, useEffect, useRef, useCallback } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { authApi } from '@/lib/api/auth'
import { useAuth } from '@/contexts/AuthContext'
import { Loader2, QrCode, CheckCircle2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'

interface LoginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

interface WSMessage {
  type: string
  data?: {
    scene?: string
    token?: string
    refresh_token?: string
    user?: { id: string; email: string; nickname: string; avatar: string }
  }
}

export default function LoginDialog({ open, onOpenChange }: LoginDialogProps) {
  const [qrcodeUrl, setQrcodeUrl] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [isExpired, setIsExpired] = useState(false)
  const [isScanned, setIsScanned] = useState(false)
  const { login } = useAuth()

  const wsRef = useRef<WebSocket | null>(null)
  const expiredTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const currentSceneRef = useRef<string | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectCountRef = useRef(0)
  const isScannedRef = useRef(false)

  const isDark = () => document.documentElement.classList.contains('dark')

  const disconnectWebSocket = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.onclose = null
      wsRef.current.close()
      wsRef.current = null
    }
    currentSceneRef.current = null
    reconnectCountRef.current = 0
  }, [])

  const connectWebSocket = useCallback((scene: string) => {
    disconnectWebSocket()

    currentSceneRef.current = scene
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws/login?scene=${scene}`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      reconnectCountRef.current = 0
    }

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        if (!msg.data || msg.data.scene !== scene) return

        if (msg.type === 'qrcode_scanned') {
          setIsScanned(true)
          isScannedRef.current = true
          if (expiredTimerRef.current) {
            clearTimeout(expiredTimerRef.current)
            expiredTimerRef.current = null
          }
        }

        if (msg.type === 'qrcode_expired') {
          if (isScannedRef.current) {
            setTimeout(() => {
              setIsExpired(true)
              disconnectWebSocket()
            }, 15000)
          } else {
            setIsExpired(true)
            disconnectWebSocket()
          }
        }

        if (msg.type === 'login_success' && msg.data) {
          const { token, refresh_token, user } = msg.data
          if (token && refresh_token && user) {
            disconnectWebSocket()
            login(token, refresh_token, user as any)
            toast.success('登录成功')
            onOpenChange(false)
          }
        }
      } catch {
        // ignore parse errors
      }
    }

    ws.onclose = () => {
      if (currentSceneRef.current === scene && reconnectCountRef.current < 5) {
        reconnectTimerRef.current = setTimeout(() => {
          reconnectCountRef.current++
          connectWebSocket(scene)
        }, Math.min(1000 * 2 ** reconnectCountRef.current, 30000))
      }
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [disconnectWebSocket, login, onOpenChange])

  const clearTimers = () => {
    if (expiredTimerRef.current) {
      clearTimeout(expiredTimerRef.current)
      expiredTimerRef.current = null
    }
  }

  const generateQRCode = useCallback(async () => {
    setIsLoading(true)
    setIsExpired(false)
    setIsScanned(false)
    isScannedRef.current = false
    try {
      const dark = isDark()
      const response = await authApi.generateQRCode({
        width: 430,
        line_color: dark ? { r: 255, g: 255, b: 255 } : { r: 0, g: 0, b: 0 },
        is_hyaline: true,
      })
      setQrcodeUrl(response.qrcode_url)
      connectWebSocket(response.scene)

      clearTimers()
      expiredTimerRef.current = setTimeout(() => {
        setIsExpired(true)
        disconnectWebSocket()
      }, 2 * 60 * 1000)
    } catch {
      toast.error('生成二维码失败')
    } finally {
      setIsLoading(false)
    }
  }, [connectWebSocket, disconnectWebSocket])

  const refreshQRCode = () => {
    setQrcodeUrl('')
    setIsExpired(false)
    setIsScanned(false)
    isScannedRef.current = false
    disconnectWebSocket()
    clearTimers()
    generateQRCode()
  }

  useEffect(() => {
    if (open) {
      generateQRCode()
    } else {
      disconnectWebSocket()
      clearTimers()
      setQrcodeUrl('')
      setIsExpired(false)
      setIsScanned(false)
      isScannedRef.current = false
    }
    return () => {
      disconnectWebSocket()
      clearTimers()
    }
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md overflow-hidden rounded-2xl">
        <DialogHeader className="space-y-1 pb-2">
          <DialogTitle className="text-lg font-medium">微信扫码登录</DialogTitle>
          <DialogDescription className="text-sm text-muted-foreground">
            {isScanned
              ? '扫码成功，请在手机上确认登录'
              : isExpired
                ? '二维码已过期，请刷新后重新扫码'
                : '使用微信扫一扫，扫描下方二维码完成登录'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center justify-center py-6 space-y-4">
          {isLoading ? (
            <div className="flex flex-col items-center space-y-4 py-8">
              <Loader2 className="h-12 w-12 animate-spin text-primary" />
              <p className="text-sm text-muted-foreground">正在生成二维码...</p>
            </div>
          ) : isExpired ? (
            <div className="flex flex-col items-center space-y-4 py-8">
              <QrCode className="h-12 w-12 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">二维码已过期</p>
              <Button onClick={refreshQRCode} variant="outline" className="gap-2">
                <RefreshCw className="h-4 w-4" />
                刷新二维码
              </Button>
            </div>
          ) : qrcodeUrl ? (
            <div className="flex flex-col items-center space-y-4">
              <div className="relative">
                <img
                  src={qrcodeUrl}
                  alt="登录二维码"
                  className="w-64 h-64 rounded-xl"
                />
                {isScanned && (
                  <div className="absolute inset-0 flex items-center justify-center bg-background/95 backdrop-blur-sm rounded-xl">
                    <div className="flex flex-col items-center space-y-3">
                      <CheckCircle2 className="h-12 w-12 text-green-500" strokeWidth={2} />
                      <div className="text-center space-y-1">
                        <p className="text-base font-medium">扫码成功</p>
                        <p className="text-sm text-muted-foreground">请在手机上确认登录</p>
                      </div>
                      <Loader2 className="h-4 w-4 text-muted-foreground animate-spin" />
                    </div>
                  </div>
                )}
              </div>
              {!isScanned && (
                <p className="text-sm text-muted-foreground">请使用微信扫一扫扫描二维码</p>
              )}
            </div>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
