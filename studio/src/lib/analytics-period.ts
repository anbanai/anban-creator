export type Granularity = 'day' | 'week' | 'month'
export type DatePreset = '7d' | '30d' | 'week' | 'month' | 'custom'
export interface AnalyticsPeriod { preset: DatePreset; from: string; to: string; granularity: Granularity }
export type AnalyticsPoint = { date: string; [key: string]: string | number | null | undefined }
export function shanghaiDay(value: string | Date) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(value))
}
export function addDays(day: string, amount: number) {
  const date = new Date(`${day}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + amount)
  return date.toISOString().slice(0, 10)
}
export function periodKey(day: string, granularity: Granularity) {
  if (granularity === 'month') return `${day.slice(0, 7)}-01`
  if (granularity === 'week') return addDays(day, -((new Date(`${day}T00:00:00Z`).getUTCDay() + 6) % 7))
  return day
}
export function rangeForPreset(preset: Exclude<DatePreset, 'custom'>, now = new Date()) {
  const to = shanghaiDay(now)
  const from = preset === '7d' ? addDays(to, -6) : preset === '30d' ? addDays(to, -29) : periodKey(to, preset === 'week' ? 'week' : 'month')
  return { from, to }
}
export function defaultAnalyticsPeriod(): AnalyticsPeriod { return { ...rangeForPreset('30d'), preset: '30d', granularity: 'day' } }
export function periodError(from: string, to: string) {
  const valid = (day: string) => /^\d{4}-\d{2}-\d{2}$/.test(day) && Number.isFinite(Date.parse(`${day}T00:00:00Z`)) && new Date(`${day}T00:00:00Z`).toISOString().slice(0, 10) === day
  if (!valid(from) || !valid(to)) return '请输入有效的开始和结束日期'
  if (from > to) return '开始日期不能晚于结束日期'
  if (Date.parse(to) - Date.parse(from) > 3660 * 86400000) return '请选择 10 年以内的日期范围'
  return ''
}
export function periodDates(from: string, to: string, granularity: Granularity) {
  if (periodError(from, to)) return []
  const dates = new Set<string>()
  const end = Date.parse(`${to}T00:00:00Z`)
  // Numeric iteration also terminates at the last four-digit ISO year.
  for (let time = Date.parse(`${from}T00:00:00Z`); time <= end; time += 86400000) {
    dates.add(periodKey(new Date(time).toISOString().slice(0, 10), granularity))
  }
  return [...dates]
}
export function bucketDailySeries(data: AnalyticsPoint[], granularity: Granularity, metrics: Array<{ key: string; aggregation: 'sum' | 'mean' }>, from?: string, to?: string): AnalyticsPoint[] {
  if (from && to && periodError(from, to)) return []
  const groups = new Map<string, AnalyticsPoint[]>()
  if (from && to) for (const date of periodDates(from, to, granularity)) groups.set(date, [])
  for (const point of data) {
    if ((from && point.date < from) || (to && point.date > to)) continue
    const key = periodKey(point.date, granularity)
    groups.set(key, [...(groups.get(key) ?? []), point])
  }
  return [...groups].sort(([a], [b]) => a.localeCompare(b)).map(([date, points]) => {
    const result: AnalyticsPoint = { date }
    for (const metric of metrics) {
      const values = points.map((p) => p[metric.key]).filter((value): value is number => typeof value === 'number' && Number.isFinite(value))
      result[metric.key] = values.length ? values.reduce((a, b) => a + b, 0) / (metric.aggregation === 'mean' ? values.length : 1) : null
    }
    return result
  })
}
