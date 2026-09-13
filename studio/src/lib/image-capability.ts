import type { ImageCapabilityOption } from '@/types'

export function explicitlyRejectsReferenceImages(capability?: ImageCapabilityOption): boolean {
  const features = capability?.generation_features
  return features?.supports_reference === false
    || (features?.max_reference_images !== undefined && features.max_reference_images < 1)
}
