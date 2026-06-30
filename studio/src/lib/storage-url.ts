/**
 * Helpers for rendering images that may live behind the JWT-authed `/files`
 * proxy OR be served as direct (signed) OSS URLs.
 *
 * Background: the OSS bucket is private, so images are proxied through the Go
 * server at `/api/v1/files/*` (the browser cannot attach an Authorization header
 * to a plain `<img>`). The backend now emits **signed** OSS URLs for image
 * fields instead — those are self-contained (signature in the query string) and
 * must be rendered directly. Re-routing a signed URL through the proxy would
 * strip the signature and re-trigger the per-user ownership check that 403s for
 * images uploaded by another account.
 */

/** True for URLs that must be fetched through the authenticated `/files/ proxy. */
export function isInternalStorageUrl(url: string): boolean {
  return url.startsWith('/files/')
}

/** A signed OSS URL carries its credentials in the query string. */
function isSignedOSSUrl(u: URL): boolean {
  return (
    u.hostname.endsWith('.aliyuncs.com') &&
    (u.searchParams.has('Signature') ||
      u.searchParams.has('OSSAccessKeyId') ||
      u.searchParams.has('Expires'))
  )
}

/**
 * Normalize a storage URL for rendering.
 *
 * - Signed OSS URLs and other absolute external URLs pass through unchanged.
 * - `/api/v1/files/...` and unsigned `*.aliyuncs.com` URLs are rewritten to the
 *   `/files/...` proxy path (the caller blob-fetches these via the authenticated
 *   http client — see `SignedImage` / `ReferenceImageUpload`).
 */
export function normalizeStorageUrl(url: string): string {
  if (url.startsWith('/api/v1/files/')) {
    return url.replace('/api/v1/files/', '/files/')
  }
  if (url.startsWith('/files/')) return url
  try {
    const u = new URL(url)
    if (u.pathname.startsWith('/api/v1/files/')) {
      return u.pathname.replace('/api/v1/files/', '/files/')
    }
    // Self-contained signed URL — render directly, do NOT route through proxy.
    if (isSignedOSSUrl(u)) return url
    if (u.hostname.endsWith('.aliyuncs.com')) {
      return '/files/' + u.pathname.slice(1)
    }
  } catch {
    // Not a parseable absolute URL (relative path etc.) — leave as-is.
  }
  return url
}
