import { useState, useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { useAuth } from '@/contexts/AuthContext'
import { PasswordInput } from '@/components/ui/PasswordInput'
import { Button } from '@/components/common/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const COUNTDOWN_SECONDS = 60

interface LoginDialogProps {
  open: boolean
  onClose: () => void
}

export default function LoginDialog({ open, onClose }: LoginDialogProps) {
  const { login } = useAuth()
  const [tab, setTab] = useState('password')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const [sendingCode, setSendingCode] = useState(false)

  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)

  useEffect(() => {
    if (countdown <= 0) return
    const timer = setInterval(() => setCountdown((c) => c - 1), 1000)
    return () => clearInterval(timer)
  }, [countdown])

  const handleSendCode = useCallback(async () => {
    if (!emailValid || countdown > 0 || sendingCode) return
    setSendingCode(true)
    try {
      await api.auth.sendVerificationCode(email)
      setCountdown(COUNTDOWN_SECONDS)
    } catch (err) {
      setError(getApiErrorMessage(err, '发送验证码失败'))
    } finally {
      setSendingCode(false)
    }
  }, [email, emailValid, countdown, sendingCode])

  if (!open) return null

  const handlePasswordSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const response = await api.auth.login(email, password)
      login(response.token, response.refresh_token, { ...response.user, has_password: response.has_password, max_invites: response.max_invites })
      onClose()
      setEmail('')
      setPassword('')
    } catch (err) {
      setError(getApiErrorMessage(err, '登录失败'))
    } finally {
      setLoading(false)
    }
  }

  const handleCodeSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const response = await api.auth.codeLogin(email, code)
      login(response.token, response.refresh_token, { ...response.user, has_password: response.has_password, max_invites: response.max_invites })
      onClose()
      setEmail('')
      setCode('')
    } catch (err) {
      setError(getApiErrorMessage(err, '登录失败'))
    } finally {
      setLoading(false)
    }
  }

  const inputClassName =
    'w-full rounded-lg border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground transition-colors focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-full max-w-md rounded-xl border border-border bg-card p-6 shadow-2xl">
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-xl font-semibold text-foreground">登录</h2>
          <button
            onClick={onClose}
            className="rounded-lg p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <svg className="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <Tabs value={tab} onValueChange={(v) => { setTab(v); setError('') }}>
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="password">密码登录</TabsTrigger>
            <TabsTrigger value="code">验证码登录</TabsTrigger>
          </TabsList>

          <TabsContent value="password">
            <form onSubmit={handlePasswordSubmit} className="space-y-4">
              <div>
                <label htmlFor="login-email" className="mb-1 block text-sm font-medium text-foreground">
                  邮箱
                </label>
                <input
                  id="login-email"
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={inputClassName}
                  placeholder="请输入邮箱地址"
                />
              </div>
              <div>
                <label htmlFor="login-password" className="mb-1 block text-sm font-medium text-foreground">
                  密码
                </label>
                <PasswordInput
                  id="login-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className={inputClassName}
                  placeholder="请输入密码"
                />
              </div>
              {error && (
                <div className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div>
              )}
              <button
                type="submit"
                disabled={loading}
                className="w-full rounded-lg bg-primary px-4 py-2 font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {loading ? '登录中...' : '登录'}
              </button>
            </form>
          </TabsContent>

          <TabsContent value="code">
            <form onSubmit={handleCodeSubmit} className="space-y-4">
              <div>
                <label htmlFor="code-email" className="mb-1 block text-sm font-medium text-foreground">
                  邮箱
                </label>
                <input
                  id="code-email"
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={inputClassName}
                  placeholder="请输入邮箱地址"
                />
              </div>
              <div>
                <label htmlFor="code-input" className="mb-1 block text-sm font-medium text-foreground">
                  验证码
                </label>
                <div className="flex gap-2">
                  <input
                    id="code-input"
                    required
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    className={inputClassName}
                    placeholder="请输入验证码"
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="default"
                    disabled={!emailValid || countdown > 0 || sendingCode}
                    loading={sendingCode}
                    onClick={handleSendCode}
                    className="shrink-0 whitespace-nowrap"
                  >
                    {countdown > 0 ? `${countdown}s` : '发送验证码'}
                  </Button>
                </div>
              </div>
              {error && (
                <div className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div>
              )}
              <button
                type="submit"
                disabled={loading}
                className="w-full rounded-lg bg-primary px-4 py-2 font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {loading ? '登录中...' : '登录'}
              </button>
            </form>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
