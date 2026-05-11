import * as React from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

type CalendarRangeValue = {
  from: string
  to: string
}

function CalendarRangePicker({
  value,
  onChange,
  onClose,
}: {
  value: CalendarRangeValue | null
  onChange: (range: CalendarRangeValue | null) => void
  onClose?: () => void
}) {
  const [from, setFrom] = React.useState(value?.from ?? "")
  const [to, setTo] = React.useState(value?.to ?? "")

  React.useEffect(() => {
    setFrom(value?.from ?? "")
    setTo(value?.to ?? "")
  }, [value?.from, value?.to])

  return (
    <div className="w-72 rounded-lg border border-border bg-popover p-3 shadow-md">
      <div className="grid gap-2">
        <label className="grid gap-1 text-xs text-muted-foreground">
          开始日期
          <Input type="date" value={from} onChange={(event) => setFrom(event.target.value)} />
        </label>
        <label className="grid gap-1 text-xs text-muted-foreground">
          结束日期
          <Input type="date" value={to} onChange={(event) => setTo(event.target.value)} />
        </label>
      </div>
      <div className="mt-3 flex justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => {
            setFrom("")
            setTo("")
            onChange(null)
            onClose?.()
          }}
        >
          清除
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!from || !to}
          onClick={() => {
            onChange({ from, to })
            onClose?.()
          }}
        >
          应用
        </Button>
      </div>
    </div>
  )
}

export { CalendarRangePicker }
