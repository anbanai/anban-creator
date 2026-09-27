export function supportsPortraitCover(type: string | undefined): boolean {
  return type === 'article' || type === 'seednote' || type === 'montage' || type === 'hypit'
}
