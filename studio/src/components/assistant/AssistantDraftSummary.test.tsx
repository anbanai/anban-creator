import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AssistantDraftSummary } from './AssistantDraftSummary'

describe('AssistantDraftSummary', () => {
  it.each(['direct', 'payload'] as const)('shows the six profile dimensions and limits from a %s draft', (shape) => {
    const payload = {
      project: { name: '春茶笔记', platform: 'wechat', instructions: '只分享亲自尝试过的泡茶方法' },
      profile: { dimensions: {
        identity: { content: { positioning: '记录真实喝茶体验' } },
        style: { content: { tone: '亲切自然' } },
        audience: { content: { target_audience: '刚开始接触茶的新手' } },
        platforms: { content: { description: '以公众号长文为主' } },
        preferences: { content: { constraints: ['避免夸大健康功效'] } },
        memory: { content: { notes: ['每周分享一款茶'] } },
      }, analysis_limits: ['受众画像尚待实际数据验证'], follow_up_questions: ['后续是否增加短视频？'] },
    }
    render(<AssistantDraftSummary mode="profile" draft={shape === 'direct'
      ? { revision: 1, ...payload }
      : { revision: 1, project: { name: '过时名称' }, payload }} />)

    for (const text of ['春茶笔记', '公众号', '只分享亲自尝试过的泡茶方法', '记录真实喝茶体验', '亲切自然', '刚开始接触茶的新手', '以公众号长文为主', '避免夸大健康功效', '每周分享一款茶', '受众画像尚待实际数据验证', '后续是否增加短视频？']) {
      expect(screen.getByText(text)).toBeVisible()
    }
    expect(screen.queryByText('过时名称')).not.toBeInTheDocument()
    expect(screen.queryByText('dimensions')).not.toBeInTheDocument()
  })

  it('shows the authoritative creation plan with readable labels and a structured outline', () => {
    render(<AssistantDraftSummary mode="creation" draft={{
      revision: 1,
      fields: { goal: '过时的目标' },
      payload: { plan: {
        goal: '发布春茶新品', content_type: 'article', topic: '第一次泡茶容易犯的错误', target_audience: '新茶客', angle: '从常见失误切入', expression_form: '步骤教程',
        profile_context: ['目标受众沿用项目画像：新茶客'], temporary_overrides: ['本次采用更轻松的语气'],
        outline: ['介绍茶叶', '演示冲泡'], style_constraints: '简洁亲切', asset_plan: '冲泡步骤图',
        acceptance_criteria: '步骤可复现', cost_estimate: '预计 5 积分', execution_options: ['先生成草稿'],
        execution_profile: 'quality', image_ratio: 'auto', image_capability_key: 'enhanced',
      } },
    }} />)
    expect(screen.getByText('创作目标')).toBeInTheDocument()
    expect(screen.getByText('发布春茶新品')).toBeInTheDocument()
    expect(screen.getByText('创作话题')).toBeInTheDocument()
    expect(screen.getByText('第一次泡茶容易犯的错误')).toBeInTheDocument()
    expect(screen.getByText('表达形式')).toBeInTheDocument()
    expect(screen.getByText('步骤教程')).toBeInTheDocument()
    expect(screen.getByText('沿用的人设信息')).toBeInTheDocument()
    expect(screen.getByText('目标受众沿用项目画像：新茶客')).toBeInTheDocument()
    expect(screen.getByText('本次临时调整')).toBeInTheDocument()
    expect(screen.getByText('本次采用更轻松的语气')).toBeInTheDocument()
    expect(screen.queryByText('过时的目标')).not.toBeInTheDocument()
    expect(screen.getByText('文章')).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: '内容结构与大纲' })).getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('预计成本')).toBeInTheDocument()
    expect(screen.getByText('预计 5 积分')).toBeInTheDocument()
    expect(screen.getByText('执行选项')).toBeInTheDocument()
    expect(screen.getByText('执行配置')).toBeInTheDocument()
    expect(screen.getByText('极致效果')).toBeInTheDocument()
    expect(screen.getByText('图片比例')).toBeInTheDocument()
    expect(screen.getByText('智能适配')).toBeInTheDocument()
    expect(screen.getByText('图像能力')).toBeInTheDocument()
    expect(screen.getByText('增强')).toBeInTheDocument()
    expect(screen.queryByText('target_audience')).not.toBeInTheDocument()
  })

  it('shows review recommendations, their evidence and limits without exposing raw JSON', () => {
    render(<AssistantDraftSummary mode="review" draft={{ revision: 1, payload: { review: {
      summary: '具体示例更容易理解', confidence: 'medium', limitations: '只有一件作品',
      dimension_recommendations: { style: {
        reason: '读者在评论中询问操作步骤', evidence: ['评论中 3 次提及步骤'],
        updates: { opening: '用真实问题开场' },
      } },
      evidence: { source: '用户提供的评论', metrics: { reads: 320 } },
    } } }} />)
    expect(screen.getByText('复盘摘要')).toBeInTheDocument()
    expect(screen.getByText('风格')).toBeInTheDocument()
    expect(screen.getByText('建议原因')).toBeInTheDocument()
    expect(screen.getByText('评论中 3 次提及步骤')).toBeInTheDocument()
    expect(screen.getByText('开头')).toBeInTheDocument()
    expect(screen.getByText('用真实问题开场')).toBeInTheDocument()
    expect(screen.getByText('中等')).toBeInTheDocument()
    expect(screen.getByText('局限与待验证事项')).toBeInTheDocument()
    expect(screen.getByText('阅读量')).toBeInTheDocument()
    expect(screen.queryByText('dimension_recommendations')).not.toBeInTheDocument()
    expect(screen.queryByText(/\{"opening"/)).not.toBeInTheDocument()
  })

  it('shows explicit empty states for profile provenance and temporary overrides', () => {
    render(<AssistantDraftSummary mode="creation" draft={{
      revision: 1,
      payload: { plan: {
        goal: '介绍新茶', content_type: 'article', topic: '冲泡方法', angle: '问题拆解', expression_form: '清单',
        profile_context: [], temporary_overrides: [],
      } },
    }} />)

    expect(screen.getByText('沿用的人设信息')).toBeInTheDocument()
    expect(screen.getByText('无额外沿用信息')).toBeInTheDocument()
    expect(screen.getByText('本次临时调整')).toBeInTheDocument()
    expect(screen.getByText('无临时调整')).toBeInTheDocument()
  })

  it('shows missing provenance labels when an older or incomplete plan omits them', () => {
    render(<AssistantDraftSummary mode="creation" draft={{
      revision: 1,
      payload: { plan: {
        goal: '介绍新茶', content_type: 'article', topic: '冲泡方法', angle: '问题拆解', expression_form: '清单',
      } },
    }} />)

    expect(screen.getByText('沿用的人设信息')).toBeInTheDocument()
    expect(screen.getAllByText('未记录')).toHaveLength(2)
    expect(screen.getByText('本次临时调整')).toBeInTheDocument()
  })

  it('explains the empty draft while the conversation is still collecting information', () => {
    render(<AssistantDraftSummary mode="creation" draft={null} compact />)
    expect(screen.getByText('讨论后，创作目标、话题、角度和表达形式会整理在这里。')).toBeInTheDocument()
  })
})
