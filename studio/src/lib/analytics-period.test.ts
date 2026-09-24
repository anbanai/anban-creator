import { describe, expect, it } from 'vitest'
import { bucketDailySeries, periodKey, periodDates, rangeForPreset, shanghaiDay } from './analytics-period'

describe('analytics periods', () => {
  it('rejects impractically large date ranges before producing chart points', () => {
    expect(periodDates('0002-01-01', '2026-09-24', 'day')).toHaveLength(0)
    expect(periodDates('9999-12-31', '9999-12-31', 'day')).toEqual(['9999-12-31'])
    expect(periodDates('2026-02-30', '2026-03-02', 'day')).toHaveLength(0)
  })
  it('uses Shanghai dates and Monday weeks across year boundaries', () => {
    expect(shanghaiDay('2026-09-23T18:00:00Z')).toBe('2026-09-24')
    expect(periodKey('2026-01-01', 'week')).toBe('2025-12-29')
    expect(rangeForPreset('7d', new Date('2026-09-24T02:00:00Z'))).toEqual({ from: '2026-09-18', to: '2026-09-24' })
  })
  it('sums daily counts, averages observed rates, and keeps absent periods null', () => {
    const series = bucketDailySeries([{ date: '2026-09-01', views: 20, rate: 0.2 }, { date: '2026-09-02', views: 30, rate: null }], 'week', [{ key: 'views', aggregation: 'sum' }, { key: 'rate', aggregation: 'mean' }], '2026-09-01', '2026-09-13')
    expect(series).toEqual([{ date: '2026-08-31', views: 50, rate: 0.2 }, { date: '2026-09-07', views: null, rate: null }])
  })
})
