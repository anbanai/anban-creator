const EMPTY_MIME_IMAGE_EXTENSIONS = new Set([
  'avif',
  'bmp',
  'gif',
  'heic',
  'heif',
  'jpeg',
  'jpg',
  'png',
  'tif',
  'tiff',
  'webp',
])

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

export function isReferenceImageFile(file: File) {
  if (file.type) {
    return file.type.startsWith('image/')
  }

  const extensionStart = file.name.lastIndexOf('.')
  if (extensionStart < 0) {
    return false
  }

  const extension = file.name.slice(extensionStart + 1).toLowerCase()
  return EMPTY_MIME_IMAGE_EXTENSIONS.has(extension)
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
