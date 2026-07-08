export const VIDEO_CREATOR = 'videocreator'
export const VIDEO_EDITOR = 'videoeditor'

export type VideoPlatform = typeof VIDEO_CREATOR | typeof VIDEO_EDITOR

export function isVideoPlatform(value?: string | null): value is VideoPlatform {
  return value === VIDEO_CREATOR || value === VIDEO_EDITOR
}

export function isVideoCreator(value?: string | null): value is typeof VIDEO_CREATOR {
  return value === VIDEO_CREATOR
}

export function isVideoEditor(value?: string | null): value is typeof VIDEO_EDITOR {
  return value === VIDEO_EDITOR
}
