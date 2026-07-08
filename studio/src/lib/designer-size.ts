const PIXEL_SIZE_PATTERN = /^\d+x\d+$/i

export function buildDesignerRequestSize(size: string, resolution: string): string {
  const normalizedSize = size.trim()
  if (!normalizedSize) return normalizedSize

  const lowerSize = normalizedSize.toLowerCase()
  if (lowerSize === 'auto' || lowerSize.startsWith('auto:')) {
    return 'auto'
  }

  if (PIXEL_SIZE_PATTERN.test(normalizedSize)) {
    return normalizedSize
  }

  const normalizedResolution = resolution.trim().toUpperCase()
  if (!normalizedResolution || normalizedResolution === '2K') {
    return normalizedSize
  }

  return `${normalizedSize}:${normalizedResolution}`
}
