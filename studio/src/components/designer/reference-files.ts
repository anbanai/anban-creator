const SUPPORTED_REFERENCE_IMAGE_MIME_TYPES = [
  'image/png',
  'image/jpeg',
  'image/jpg',
  'image/gif',
  'image/webp',
  'image/bmp',
] as const

const SUPPORTED_REFERENCE_IMAGE_EXTENSIONS = [
  'png',
  'jpeg',
  'jpg',
  'gif',
  'webp',
  'bmp',
] as const

export const REFERENCE_IMAGE_ACCEPT = [
  ...SUPPORTED_REFERENCE_IMAGE_MIME_TYPES,
  ...SUPPORTED_REFERENCE_IMAGE_EXTENSIONS.map((extension) => `.${extension}`),
].join(',')

const SUPPORTED_REFERENCE_IMAGE_MIME_TYPE_SET = new Set<string>(
  SUPPORTED_REFERENCE_IMAGE_MIME_TYPES,
)
const SUPPORTED_REFERENCE_IMAGE_EXTENSION_SET = new Set<string>(
  SUPPORTED_REFERENCE_IMAGE_EXTENSIONS,
)

export interface ReferenceAdmissionResult {
  files: File[]
  accepted: number
  rejectedNonImages: number
  rejectedDuplicates: number
  rejectedOverflow: number
}

export function referenceFileKey(file: File) {
  return [file.name, file.size, file.lastModified, file.type].join('\u0000')
}

export function isSupportedReferenceImageMimeType(type: string) {
  return SUPPORTED_REFERENCE_IMAGE_MIME_TYPE_SET.has(type.toLowerCase())
}

export function isReferenceImageFile(file: File) {
  if (file.type) {
    return isSupportedReferenceImageMimeType(file.type)
  }

  const extensionStart = file.name.lastIndexOf('.')
  if (extensionStart < 0) {
    return false
  }

  const extension = file.name.slice(extensionStart + 1).toLowerCase()
  return SUPPORTED_REFERENCE_IMAGE_EXTENSION_SET.has(extension)
}

export function admitReferenceFiles(
  currentFiles: File[],
  incomingFiles: File[],
  maxFiles: number,
): ReferenceAdmissionResult {
  const safeMax = Math.max(0, maxFiles)
  const files = currentFiles.slice(0, safeMax)
  const seenKeys = new Set(files.map(referenceFileKey))
  let accepted = 0
  let rejectedNonImages = 0
  let rejectedDuplicates = 0
  let rejectedOverflow = currentFiles.length - files.length

  for (const file of incomingFiles) {
    if (!isReferenceImageFile(file)) {
      rejectedNonImages += 1
      continue
    }

    const key = referenceFileKey(file)
    if (seenKeys.has(key)) {
      rejectedDuplicates += 1
      continue
    }
    seenKeys.add(key)

    if (files.length >= safeMax) {
      rejectedOverflow += 1
      continue
    }

    files.push(file)
    accepted += 1
  }

  return {
    files,
    accepted,
    rejectedNonImages,
    rejectedDuplicates,
    rejectedOverflow,
  }
}
