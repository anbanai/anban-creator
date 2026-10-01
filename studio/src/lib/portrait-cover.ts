export function supportsPortraitCover(type: string | undefined): boolean {
  return type === 'wechat' || type === 'wechat-article' || type === 'wechat-picture' || type === 'seednote' || type === 'montage' || type === 'hypit'
}
