// 任务状态
export const taskStatusLabel: Record<string, string> = {
  pending: '待执行',
  running: '运行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

// 计划状态
export const planStatusLabel: Record<string, string> = {
  active: '运行中',
  paused: '已暂停',
  completed: '已完成',
}

// 内容类型
export const contentTypeLabel: Record<string, string> = {
  rednote: '小红书',
  article: '公众号文章',
  xls: '小绿书',
}

// 内容类型选项（用于 Select 组件）
export const contentTypeOptions = [
  { value: 'rednote', label: '小红书' },
  { value: 'article', label: '公众号文章' },
  { value: 'xls', label: '小绿书' },
]

// 时间线项类型
export const timelineItemTypeLabel: Record<string, string> = {
  task: '任务',
  plan: '计划',
}

// 星期名称
export const weekDayLabels = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

// 格式化日期为中文格式（月/日 时:分）
export function formatDateTimeCN(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 格式化完整日期时间
export function formatFullDateTimeCN(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

// 格式化日期标签（今天、明天、昨天、或中文日期）
export function formatDateLabelCN(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const diff = d.getTime() - today.getTime()
  if (diff === 0) return '今天'
  if (diff === 86400000) return '明天'
  if (diff === -86400000) return '昨天'
  return d.toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'short' })
}

// 格式化月份
export function formatMonthCN(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' })
}

// 格式化时间
export function formatTimeCN(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}
