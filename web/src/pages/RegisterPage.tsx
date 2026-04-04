import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import { useAuth } from '@/contexts/AuthContext'

export default function RegisterPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const response = await api.auth.register(email, password, nickname || undefined)
      // Auto-login after successful registration
      login(response.token, response.refresh_token, response.user)
      navigate('/', { replace: true })
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message || '注册失败')
      } else {
        setError('注册失败，请重试。')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-900 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-bold text-blue-400">AnbanWriter</h1>
          <p className="mt-2 text-sm text-gray-400">创建你的账号</p>
        </div>

        <form
          onSubmit={handleSubmit}
          className="rounded-xl border border-gray-700 bg-gray-800 p-6 shadow-lg"
        >
          <div className="space-y-4">
            <div>
              <label htmlFor="email" className="mb-1 block text-sm font-medium text-gray-300">
                邮箱
              </label>
              <input
                id="email"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-gray-100 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="请输入邮箱地址"
              />
            </div>

            <div>
              <label htmlFor="nickname" className="mb-1 block text-sm font-medium text-gray-300">
                昵称
              </label>
              <input
                id="nickname"
                type="text"
                value={nickname}
                onChange={(e) => setNickname(e.target.value)}
                className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-gray-100 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="可选的显示名称"
              />
            </div>

            <div>
              <label htmlFor="password" className="mb-1 block text-sm font-medium text-gray-300">
                密码
              </label>
              <input
                id="password"
                type="password"
                required
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-gray-100 placeholder-gray-400 transition-colors focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="至少 8 个字符"
              />
            </div>

            {error && (
              <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="w-full rounded-lg bg-blue-600 px-4 py-2 font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {loading ? '创建中...' : '创建账号'}
            </button>
          </div>

          <p className="mt-4 text-center text-sm text-gray-400">
            已有账号？{' '}
            <Link to="/login" className="text-blue-400 hover:text-blue-300">
              登录
            </Link>
          </p>
        </form>
      </div>
    </div>
  )
}
