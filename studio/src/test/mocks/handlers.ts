import { http, HttpResponse } from 'msw'
import type {
  User,
  CreditBalance,
  SignInStatus,
  PaginatedResponse,
  CreditTransaction,
  Plan,
  Task,
  Project,
  ProjectDetail,
  PlatformConfig,
  APIKey,
} from '@/types'

// --- Mock Data ---

export const mockUser: User = {
  id: '1',
  email: 'test@example.com',
  phone: '',
  nickname: '测试用户',
  avatar: '',
  credits_balance: 10000,
  tier: 'pro',
  max_concurrent_limit: 5,
  invite_code: 'AB2C4D6E',
  invite_count: 0,
  max_invites: 3,
  has_password: true,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

export const mockBalance: CreditBalance = { balance: 10000 }
export const mockSignInStatus: SignInStatus = { signed_in_today: false }

export const mockTransactions: PaginatedResponse<CreditTransaction> = {
  items: [
    {
      id: 1,
      user_id: '1',
      type: 'sign_in',
      amount: 100,
      balance_after: 10100,
      description: '每日签到',
      created_at: '2025-01-15T08:00:00Z',
    },
  ],
  total: 1,
}

export const mockPlans: PaginatedResponse<Plan> = {
  items: [
    {
      id: 'plan-1',
      type: 'article',
      title: '测试计划',
      description: '',
      cron_expr: '0 9 * * 1',
      prompt: '',
      status: 'active',
      next_run_at: '2025-01-20T09:00:00Z',
      project_id: 'ch-1',
      created_at: '2025-01-10T00:00:00Z',
      updated_at: '2025-01-10T00:00:00Z',
    },
  ],
  total: 1,
}

export const mockTasks: PaginatedResponse<Task> = {
  items: [
    {
      id: 'task-1',
      type: 'article',
      prompt: '测试任务',
      status: 'completed',
      progress: 100,
      error: null,
      plan_id: null,
      project_id: 'ch-1',
      result: { files: null, output: '测试输出' },
      published: false,
      published_at: null,
      created_at: '2025-01-15T10:00:00Z',
      started_at: '2025-01-15T10:00:05Z',
      completed_at: '2025-01-15T10:05:00Z',
    },
  ],
  total: 1,
}

export const mockProjects: Project[] = [
  {
    id: 'ch-1',
    user_id: '1',
    platform: 'article',
    name: '测试项目',
    avatar_url: '',
    profile_url: 'https://mp.weixin.qq.com/test',
    instructions: '测试定位',
    keywords: '测试',
    visual_style: '',
    writer: '',
    theme: '',
    author: '作者',
    template_id: '',
    reference_image_url: '',
    image_ratio: '16:9',
    max_concurrent_tasks: 2,
    config: { wechat_app_id: 'wx123' },
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
]

export const mockProjectDetail: ProjectDetail = {
  project: mockProjects[0],
  stats: {
    total_tasks: 10,
    completed_tasks: 8,
    failed_tasks: 1,
    running_tasks: 0,
    pending_tasks: 1,
    success_rate: 80,
    last_activity_at: '2025-01-15T10:00:00Z',
  },
}

export const mockPlatformConfigs: PlatformConfig[] = [
  {
    id: 'article',
    label: '公众号',
    badge_variant: 'success',
    supports_publishing: true,
    supports_auto_fetch: true,
    profile_url_pattern: 'https://mp.weixin.qq.com/*',
    default_image_ratio: '16:9',
    fields: [],
  },
  {
    id: 'seednote',
    label: '种草笔记',
    badge_variant: 'danger',
    supports_publishing: false,
    supports_auto_fetch: false,
    profile_url_pattern: 'https://www.xiaohongshu.com/*',
    default_image_ratio: '3:4',
    fields: [],
  },
  {
    id: 'moments',
    label: '朋友圈',
    badge_variant: 'secondary',
    supports_publishing: false,
    supports_auto_fetch: false,
    profile_url_pattern: '',
    default_image_ratio: '3:4',
    fields: [],
  },
]

export const mockApiKeys: APIKey[] = [
  {
    id: 'key-1',
    user_id: '1',
    name: '测试 Key',
    key_prefix: 'abw_',
    last_used_at: null,
    created_at: '2025-01-15T00:00:00Z',
  },
]

// --- Handlers ---

export const handlers = [
  // Auth
  http.post('/api/v1/auth/login', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        token: 'test-token',
        refresh_token: 'test-refresh',
        expires_at: Date.now() + 3600000,
        user: mockUser,
      },
    })
  }),

  http.post('/api/v1/auth/register', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        token: 'test-token',
        refresh_token: 'test-refresh',
        expires_at: Date.now() + 3600000,
        user: mockUser,
      },
    })
  }),

  http.post('/api/v1/auth/logout', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.get('/api/v1/auth/me', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockUser })
  }),

  http.post('/api/v1/auth/send-code', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: { msg: '验证码已发送' } })
  }),

  // Credits
  http.get('/api/v1/credits/balance', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockBalance })
  }),

  http.get('/api/v1/credits/sign-in/status', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockSignInStatus })
  }),

  http.post('/api/v1/credits/sign-in', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: { balance: 2048 } })
  }),

  http.get('/api/v1/credits/transactions', async ({ request }) => {
    const url = new URL(request.url)
    const page = url.searchParams.get('page') || '1'
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: { ...mockTransactions, items: page === '1' ? mockTransactions.items : [] },
    })
  }),

  http.get('/api/v1/credits/pricing', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        task_costs: {
          article: 4000,
          seednote: 3600,
          moments: 3000,
          viral_analysis: 1200,
        },
        model_costs: {},
        recharge_tiers: [
          { key: 'basic', label: '基础包', price_cny: 10, credits: 10000, bonus_credits: 0, enabled: true },
          { key: 'standard', label: '标准包', price_cny: 50, credits: 52000, bonus_credits: 2000, enabled: true },
          { key: 'pro', label: '进阶包', price_cny: 100, credits: 110000, bonus_credits: 10000, enabled: true },
        ],
        income: {
          daily_sign_in: 100,
          register_bonus: 1000,
          invite_reward: 1000,
        },
      },
    })
  }),

  // Plans
  http.get('/api/v1/plans', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockPlans })
  }),

  http.post('/api/v1/plans', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockPlans.items[0] })
  }),

  http.post('/api/v1/plans/:id/pause', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.post('/api/v1/plans/:id/resume', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.delete('/api/v1/plans/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  // Tasks
  http.get('/api/v1/tasks', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockTasks })
  }),

  http.post('/api/v1/tasks', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockTasks.items[0] })
  }),

  http.get('/api/v1/tasks/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockTasks.items[0] })
  }),

  http.post('/api/v1/tasks/:id/cancel', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.patch('/api/v1/tasks/:id/published', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: { published: true } })
  }),

  http.get('/api/v1/tasks/:id/files', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: [] })
  }),

  http.get('/api/v1/tasks/:id/video-production', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        task_id: 'task-1',
        artifacts: {},
        retake_actions: ['keep', 'fix_in_post', 'edit', 're_roll', 'rewrite'],
        next_actions: ['continue_editing', 'generate_cover', 'export_capcut_draft'],
      },
    })
  }),

  http.get('/api/v1/videocreator/playbooks', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        items: [{
          key: 'live_selling',
          label: '直播带货',
          creative_type: 'product_demo',
          purpose: 'ecommerce',
          required_reference_roles: ['product appearance', 'action', 'voice tone'],
          default_ratio: '9:16',
          prompt_scaffold: '黄金三秒开场、一个核心卖点、亲手展示、明确 CTA。',
          qc_focus: ['产品保真', '口播可信', 'CTA'],
          risk_notes: ['卖点必须来自用户 brief。'],
        }],
      },
    })
  }),

  http.post('/api/v1/viral-analyses', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        id: 'analysis-1',
        user_id: '1',
        source_type: 'note',
        source_url: 'https://www.xiaohongshu.com/explore/mock',
        source_data: {},
        analysis_result: null,
        status: 'pending',
        created_at: '2025-01-15T10:00:00Z',
        updated_at: '2025-01-15T10:00:00Z',
      },
    })
  }),

  // Projects
  http.get('/api/v1/projects', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockProjects })
  }),

  http.get('/api/v1/projects/stats', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: {
        'ch-1': mockProjectDetail.stats,
      },
    })
  }),

  http.get('/api/v1/projects/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockProjectDetail })
  }),

  http.post('/api/v1/projects', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockProjects[0] })
  }),

  http.put('/api/v1/projects/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockProjects[0] })
  }),

  http.patch('/api/v1/projects/:id/archive', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.patch('/api/v1/projects/:id/restore', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.delete('/api/v1/projects/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: null })
  }),

  http.get('/api/v1/projects/platform-configs', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: mockPlatformConfigs })
  }),

  // API Keys
  http.get('/api/v1/api-keys', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: { items: mockApiKeys } })
  }),

  http.post('/api/v1/api-keys', async () => {
    return HttpResponse.json({
      code: 0,
      msg: 'ok',
      data: { id: 'key-2', name: '新 Key', key_prefix: 'abw_', key: 'abw_test_key', created_at: new Date().toISOString() },
    })
  }),

  http.delete('/api/v1/api-keys/:id', async () => {
    return HttpResponse.json({ code: 0, msg: 'ok', data: { revoked: true } })
  }),
]
