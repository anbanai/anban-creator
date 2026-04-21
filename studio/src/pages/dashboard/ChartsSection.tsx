import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
} from 'recharts'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'

const CHART_COLORS = [
  'hsl(270, 60%, 55%)',
  'hsl(170, 50%, 55%)',
  'hsl(60, 60%, 55%)',
  'hsl(330, 50%, 55%)',
]

const tooltipStyle = {
  backgroundColor: 'var(--background)',
  color: 'var(--foreground)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius)',
  fontSize: '12px',
}

interface ChartsSectionProps {
  trendData: { date: string; count: number }[]
  statusData: { name: string; value: number }[]
}

export default function ChartsSection({ trendData, statusData }: ChartsSectionProps) {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {/* Task trend line chart */}
      <Card>
        <CardHeader>
          <CardTitle>任务趋势（近30天）</CardTitle>
        </CardHeader>
        <CardContent>
          {trendData.length > 0 ? (
            <ResponsiveContainer width="100%" height={240}>
              <LineChart data={trendData}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  dataKey="date"
                  tick={{ fontSize: 12 }}
                  stroke="var(--muted-foreground)"
                  interval="preserveStartEnd"
                />
                <YAxis
                  allowDecimals={false}
                  tick={{ fontSize: 12 }}
                  stroke="var(--muted-foreground)"
                />
                <Tooltip contentStyle={tooltipStyle} />
                <Line
                  type="monotone"
                  dataKey="count"
                  name="任务数"
                  stroke={CHART_COLORS[0]}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 4, fill: CHART_COLORS[0] }}
                />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">
              暂无数据
            </div>
          )}
        </CardContent>
      </Card>

      {/* Status distribution donut chart */}
      <Card>
        <CardHeader>
          <CardTitle>状态分布</CardTitle>
        </CardHeader>
        <CardContent>
          {statusData.length > 0 ? (
            <div className="flex items-center justify-center">
              <ResponsiveContainer width="100%" height={240}>
                <PieChart>
                  <Pie
                    data={statusData}
                    cx="50%"
                    cy="50%"
                    innerRadius={60}
                    outerRadius={90}
                    paddingAngle={4}
                    dataKey="value"
                    nameKey="name"
                  >
                    {statusData.map((_, index) => (
                      <Cell key={`cell-${index}`} fill={CHART_COLORS[index % CHART_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip contentStyle={tooltipStyle} />
                </PieChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">
              暂无数据
            </div>
          )}
          {statusData.length > 0 && (
            <div className="mt-2 flex items-center justify-center gap-4">
              {statusData.map((entry, index) => (
                <div key={entry.name} className="flex items-center gap-1.5 text-xs">
                  <span
                    className="inline-block h-2.5 w-2.5 rounded-full"
                    style={{ backgroundColor: CHART_COLORS[index % CHART_COLORS.length] }}
                  />
                  <span className="text-muted-foreground">{entry.name}</span>
                  <span className="font-medium text-foreground">{entry.value}</span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
