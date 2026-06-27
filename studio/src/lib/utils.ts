import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// 将美元金额格式化为紧凑的展示串：极小额保留 4 位（token 级精度），否则 2 位。
// 与 UsagePage 的成本展示口径一致，作为 task.cost / 用量统计的统一格式化器。
export function formatUSD(n: number): string {
  if (n < 0.01) return `$${n.toFixed(4)}`
  return `$${n.toFixed(2)}`
}
