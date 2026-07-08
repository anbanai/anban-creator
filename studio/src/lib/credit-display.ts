import type { CreditTransaction } from '@/types'
import { operationLabel } from './labels'

const taskLabels: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号文章',
  moments: '朋友圈',
  ecommerce: '电商出图',
  viral_analysis: '爆文拆解',
  videocreator: 'AI 视频生成',
  videoeditor: '视频剪辑后期',
}

function creditAmount(tx: CreditTransaction, fallback?: string): number {
  const parsed = fallback ? Number.parseInt(fallback, 10) : Number.NaN
  return Number.isFinite(parsed) && parsed > 0 ? parsed : Math.abs(tx.amount)
}

function taskLabel(type: string): string {
  return taskLabels[type] ?? type
}

function operationDisplayLabel(type: string): string {
  return operationLabel[type] ?? type
}

export function formatCreditDescription(tx: CreditTransaction): string {
  const description = tx.description.trim()

  const goalTask = description.match(/^强目标任务扣费 \(([^,)]+), [×x](\d+)\) -(\d+)$/)
  if (goalTask) {
    const [, type, multiplier, amount] = goalTask
    return `生成${taskLabel(type)}（强目标 x${multiplier}）扣除积分${creditAmount(tx, amount)}`
  }

  const legacyTask = description.match(/^(?:任务扣费|任务消耗|套餐扣费) \(([^)]+)\) -(\d+)$/)
  if (legacyTask) {
    const [, type, amount] = legacyTask
    return `生成${taskLabel(type)}扣除积分${creditAmount(tx, amount)}`
  }

  const legacyOperation = description.match(/^操作扣费 \(([^)]+)\) -(\d+)$/)
  if (legacyOperation) {
    const [, type, amount] = legacyOperation
    return `${operationDisplayLabel(type)}扣除积分${creditAmount(tx, amount)}`
  }

  return description
}
