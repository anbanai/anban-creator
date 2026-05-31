import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import AnalysisReport from './AnalysisReport'
import type { AnalysisResult } from '@/types'

describe('AnalysisReport', () => {
  it('renders evidence-driven report sections from the strict schema', () => {
    render(<AnalysisReport analysis={analysisResult()} />)

    expect(screen.getByText('爆款结论摘要')).toBeInTheDocument()
    expect(screen.getByText('证据表')).toBeInTheDocument()
    expect(screen.getByText('选题角度')).toBeInTheDocument()
    expect(screen.getByText('评论信号')).toBeInTheDocument()
    expect(screen.getByText('模板预览')).toBeInTheDocument()
    expect(screen.getByText('Rewrite Constraints')).toBeInTheDocument()
    expect(screen.getByText('Do Not Copy')).toBeInTheDocument()
    expect(screen.queryByText('爆文因子雷达图')).not.toBeInTheDocument()
  })

  it('does not render legacy analysis shapes', () => {
    render(<AnalysisReport analysis={{ overall_score: 88, viral_factors: { topic: 90 } } as unknown as AnalysisResult} />)

    expect(screen.getByText('报告结构无效')).toBeInTheDocument()
    expect(screen.getByText('旧版本报告不再支持，请重新拆解。')).toBeInTheDocument()
    expect(screen.queryByText('爆文因子雷达图')).not.toBeInTheDocument()
  })

  it('rejects incomplete new-shape reports instead of crashing', () => {
    const invalid = analysisResult() as unknown as Record<string, unknown>
    delete invalid.clone_suggestions

    render(<AnalysisReport analysis={invalid as unknown as AnalysisResult} />)

    expect(screen.getByText('报告结构无效')).toBeInTheDocument()
    expect(screen.getByText('旧版本报告不再支持，请重新拆解。')).toBeInTheDocument()
  })
})

function analysisResult(): AnalysisResult {
  return {
    summary: ['低门槛人群承诺带来点击', '清单正文增强收藏', '标签组合覆盖搜索'],
    evidence_table: [
      { claim: '标题降低理解门槛', evidence: '标题原文「新手7天学会选咖啡豆」', source: 'title' },
      { claim: '收藏理由明确', evidence: '正文片段「1. 看烘焙度」', source: 'body' },
    ],
    dimensions: [
      dimension('topic_angle', '观察到标题原文「新手7天学会选咖啡豆」直接点名新手身份。'),
      dimension('title', '观察到标题原文「新手7天学会选咖啡豆」使用人群+时间+结果结构。'),
      dimension('cover', '观察到封面数据为 https://example.com/cover.jpg。'),
      dimension('body', '观察到正文片段「先说结论：别只看产区」。'),
      dimension('interaction', '观察到互动数据点赞1200、收藏980、评论0。'),
      dimension('tags', '观察到标签「咖啡,咖啡豆,新手咖啡」。'),
      dimension('comment_signals', '观察到评论数据 comment_count=0，缺少真实评论文本。'),
    ],
    clone_suggestions: {
      title: ['人群+时间+结果'],
      body: ['开头先给结论'],
      cover: ['大字标题+产品图'],
      tags: ['大词+垂直词+长尾词'],
      interaction: ['结尾开放式问题'],
    },
    risks: ['不要复制源作者经历'],
    recommended_clone_depth: 'style-only',
    overall_score: {
      score: 79,
      confidence: 'medium',
      evidence_count: 12,
      missing_data: ['无评论内容'],
      why_not_higher: '评论和封面细节不足',
    },
    viral_template: {
      title_template: '人群+时间+结果',
      cover_template: '大字标题+主视觉',
      body_template: '结论先行+清单',
      interaction_template: '开放式提问',
      tag_template: '大词+垂直词+长尾词',
      audience_insight: '新手怕踩坑',
      viral_mechanism: '降低门槛并给收藏理由',
      rewrite_constraints: ['换产品和个人经验'],
      do_not_copy: ['不要复制原句'],
      recommended_clone_depth: 'style-only',
      confidence: 'medium',
    },
    template_meta: {
      type: 'seednote',
      name: '新手避坑模板',
      category: 'viral_analysis',
      source_feed_id: 'note-1',
      source_url: 'https://example.com/note/1',
      tags: ['新手'],
      template_hash: 'hash',
      save_eligible: true,
    },
  }
}

function dimension(name: AnalysisResult['dimensions'][number]['name'], observation: string): AnalysisResult['dimensions'][number] {
  return {
    name,
    observation,
    mechanism: '这个元素可能降低理解成本并提高收藏意愿。',
    transferability: 'high',
    action: '下一篇保留结构但替换产品和个人经验。',
  }
}
