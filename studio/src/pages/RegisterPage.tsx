import { useState, useEffect, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { useAuth } from '@/contexts/AuthContext'
import { registerSchema, type RegisterFormValues } from '@/lib/schemas'
import { Input } from '@/components/ui/Input'
import { Button } from '@/components/ui/Button'
import { Card, CardContent } from '@/components/ui/Card'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'

const COUNTDOWN_SECONDS = 60

export default function RegisterPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [countdown, setCountdown] = useState(0)
  const [sendingCode, setSendingCode] = useState(false)

  const form = useForm<RegisterFormValues>({
    resolver: zodResolver(registerSchema),
    defaultValues: { email: '', code: '', password: '', nickname: '' },
  })

  const emailValue = form.watch('email')
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailValue)

  // Countdown timer
  useEffect(() => {
    if (countdown <= 0) return
    const timer = setInterval(() => setCountdown((c) => c - 1), 1000)
    return () => clearInterval(timer)
  }, [countdown])

  const handleSendCode = useCallback(async () => {
    if (!emailValid || countdown > 0 || sendingCode) return
    setSendingCode(true)
    try {
      await api.auth.sendVerificationCode(emailValue)
      setCountdown(COUNTDOWN_SECONDS)
      toast.success('验证码已发送')
    } catch (err) {
      toast.error(getApiErrorMessage(err, '发送验证码失败，请稍后重试'))
    } finally {
      setSendingCode(false)
    }
  }, [emailValue, emailValid, countdown, sendingCode])

  async function onSubmit(values: RegisterFormValues) {
    try {
      const response = await api.auth.register(values.email, values.password, values.code, values.nickname || undefined)
      login(response.token, response.refresh_token, response.user)
      navigate('/', { replace: true })
    } catch (err) {
      toast.error(getApiErrorMessage(err, '注册失败，请重试。'))
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-bold tracking-tight text-foreground">
            案板创作助手
          </h1>
          <div className="mx-auto mt-2 h-0.5 w-8 rounded-full bg-primary" />
          <p className="mt-4 text-sm text-muted-foreground">创建你的账号</p>
        </div>
        <Card>
          <CardContent className="pt-6">
            <Form {...form}>
              <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
                <FormField control={form.control} name="email" render={({ field }) => (
                  <FormItem>
                    <FormLabel>邮箱</FormLabel>
                    <FormControl><Input type="email" placeholder="请输入邮箱地址" {...field} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
                <FormField control={form.control} name="code" render={({ field }) => (
                  <FormItem>
                    <FormLabel>验证码</FormLabel>
                    <div className="flex gap-2">
                      <FormControl>
                        <Input placeholder="请输入验证码" className="flex-1" {...field} />
                      </FormControl>
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
                    <FormMessage />
                  </FormItem>
                )} />
                <FormField control={form.control} name="nickname" render={({ field }) => (
                  <FormItem>
                    <FormLabel>昵称</FormLabel>
                    <FormControl><Input placeholder="可选的显示名称" {...field} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
                <FormField control={form.control} name="password" render={({ field }) => (
                  <FormItem>
                    <FormLabel>密码</FormLabel>
                    <FormControl><Input type="password" placeholder="至少 8 个字符" {...field} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
                <Button type="submit" className="w-full" loading={form.formState.isSubmitting}>
                  创建账号
                </Button>
              </form>
            </Form>
            <p className="mt-4 text-center text-sm text-muted-foreground">
              已有账号？ <Link to="/login" className="text-primary hover:text-primary/80">登录</Link>
            </p>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
