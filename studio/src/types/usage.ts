export interface TypeStatEntry {
  count: number
}

export interface UsageStats {
  total_tasks: number
  by_type?: Record<string, TypeStatEntry>
}
