import { useState, useEffect, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { useAuth } from '@/contexts/AuthContext'
import { loginSchema, codeLoginSchema, type LoginFormValues, type CodeLoginFormValues } from '@/lib/schemas'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/PasswordInput'
import { Button } from '@/components/common/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import AuthLayout from '@/components/auth/AuthLayout'

const COUNTDOWN_SECONDS = 60

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: { pathname: string } })?.from?.pathname || '/'
  const [searchParams] = useSearchParams()
  const inviteCode = searchParams.get('invite')?.toUpperCase() || undefined

  const passwordForm = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  })

  const codeForm = useForm<CodeLoginFormValues>({
    resolver: zodResolver(codeLoginSchema),
    defaultValues: { email: '', code: '' },
  })

  const [countdown, setCountdown] = useState(0)
  const [sendingCode, setSendingCode] = useState(false)

  const codeEmail = codeForm.watch('email')
  const codeEmailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(codeEmail)

  useEffect(() => {
    if (countdown <= 0) return
    const timer = setInterval(() => setCountdown((c) => c - 1), 1000)
    return () => clearInterval(timer)
  }, [countdown])

  const handleSendCode = useCallback(async () => {
    if (!codeEmailValid || countdown > 0 || sendingCode) return
    setSendingCode(true)
    try {
      await api.auth.sendVerificationCode(codeEmail)
      setCountdown(COUNTDOWN_SECONDS)
      toast.success('验证码已发送')
    } catch (err) {
      toast.error(getApiErrorMessage(err, '发送验证码失败，请稍后重试'))
    } finally {
      setSendingCode(false)
    }
  }, [codeEmail, codeEmailValid, countdown, sendingCode])

  async function onPasswordSubmit(values: LoginFormValues) {
    try {
      const response = await api.auth.login(values.email, values.password)
      login(response.token, response.refresh_token, { ...response.user, has_password: response.has_password, max_invites: response.max_invites })
      navigate(from, { replace: true })
    } catch (err) {
      toast.error(getApiErrorMessage(err, '登录失败，请重试。'))
    }
  }

  async function onCodeSubmit(values: CodeLoginFormValues) {
    try {
      const response = await api.auth.codeLogin(values.email, values.code, inviteCode)
      login(response.token, response.refresh_token, { ...response.user, has_password: response.has_password, max_invites: response.max_invites })
      navigate(from, { replace: true })
    } catch (err) {
      toast.error(getApiErrorMessage(err, '登录失败，请重试。'))
    }
  }

  return (
    <AuthLayout
      title="欢迎回来"
      subtitle="登录你的账号继续创作"
      footerText="还没有账号？"
      footerLinkText="立即注册"
      footerLinkTo="/register"
    >
      {inviteCode ? (
        <div className="mb-6">
          <FormLabel>邀请码</FormLabel>
          <Input readOnly value={inviteCode} autoComplete="off" className="mt-1.5 uppercase" tabIndex={-1} />
        </div>
      ) : null}

      <Tabs defaultValue="password" className="w-full">
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="password">密码登录</TabsTrigger>
          <TabsTrigger value="code">验证码登录</TabsTrigger>
        </TabsList>

        <TabsContent value="password">
          <Form {...passwordForm}>
            <form onSubmit={passwordForm.handleSubmit(onPasswordSubmit)} className="space-y-4">
              <FormField
                control={passwordForm.control}
                name="email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>邮箱</FormLabel>
                    <FormControl>
                      <Input type="email" autoComplete="email" placeholder="请输入邮箱地址" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={passwordForm.control}
                name="password"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>密码</FormLabel>
                    <FormControl>
                      <PasswordInput autoComplete="current-password" placeholder="请输入密码" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" className="w-full" loading={passwordForm.formState.isSubmitting}>
                登录
              </Button>
            </form>
          </Form>
        </TabsContent>

        <TabsContent value="code">
          <Form {...codeForm}>
            <form onSubmit={codeForm.handleSubmit(onCodeSubmit)} className="space-y-4">
              <FormField
                control={codeForm.control}
                name="email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>邮箱</FormLabel>
                    <FormControl>
                      <Input type="email" autoComplete="email" placeholder="请输入邮箱地址" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={codeForm.control}
                name="code"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>验证码</FormLabel>
                    <div className="flex gap-2">
                      <FormControl>
                        <Input inputMode="numeric" autoComplete="one-time-code" placeholder="请输入验证码" className="flex-1" {...field} />
                      </FormControl>
                      <Button
                        type="button"
                        variant="outline"
                        size="default"
                        disabled={!codeEmailValid || countdown > 0 || sendingCode}
                        loading={sendingCode}
                        onClick={handleSendCode}
                        className="shrink-0 whitespace-nowrap"
                      >
                        {countdown > 0 ? `${countdown}s` : '发送验证码'}
                      </Button>
                    </div>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" className="w-full" loading={codeForm.formState.isSubmitting}>
                登录
              </Button>
            </form>
          </Form>
        </TabsContent>
      </Tabs>

      {/* Divider */}
      <div className="my-4 flex items-center gap-3">
        <div className="h-px flex-1 bg-border" />
        <span className="text-xs text-muted-foreground">其他方式</span>
        <div className="h-px flex-1 bg-border" />
      </div>

      {/* WeChat login */}
      <Button
        type="button"
        variant="outline"
        disabled
        className="w-full border-green-500/30 bg-green-50 text-green-700 opacity-70 dark:border-green-600/40 dark:bg-green-900/20 dark:text-green-400"
      >
        <svg className="mr-1.5 h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
          <path d="M8.691 2.188C3.891 2.188 0 5.476 0 9.53c0 2.212 1.17 4.203 3.002 5.55a.59.59 0 01.213.665l-.39 1.48c-.019.07-.048.141-.048.213 0 .163.13.295.29.295a.326.326 0 00.167-.054l1.903-1.114a.864.864 0 01.717-.098 10.16 10.16 0 002.837.403c.276 0 .543-.027.811-.05-.857-2.578.157-4.972 1.932-6.446 1.703-1.415 3.882-1.98 5.853-1.838-.576-3.583-4.196-6.348-8.596-6.348zM5.785 5.991c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 01-1.162 1.178A1.17 1.17 0 014.623 7.17c0-.651.52-1.18 1.162-1.18zm5.813 0c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 01-1.162 1.178 1.17 1.17 0 01-1.162-1.178c0-.651.52-1.18 1.162-1.18zm3.825 2.97c-3.792 0-6.876 2.599-6.876 5.81 0 3.211 3.084 5.81 6.876 5.81a8.08 8.08 0 002.276-.323.67.67 0 01.559.076l1.483.869a.258.258 0 00.13.042.228.228 0 00.226-.23c0-.056-.023-.11-.038-.166l-.305-1.153a.46.46 0 01.166-.518C21.138 18.852 22 17.165 22 15.27c0-3.211-3.084-5.81-6.876-5.81h.3zm-2.583 2.97c.502 0 .909.414.909.924a.917.917 0 01-.909.923.917.917 0 01-.909-.923c0-.51.407-.924.909-.924zm4.554 0c.502 0 .909.414.909.924a.917.917 0 01-.909.923.917.917 0 01-.909-.923c0-.51.407-.924.909-.924z" />
        </svg>
        微信一键登录（即将上线）
      </Button>
    </AuthLayout>
  )
}
