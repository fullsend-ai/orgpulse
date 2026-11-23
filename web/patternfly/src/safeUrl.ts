const SAFE_LINK_PROTOCOLS = new Set(['http:', 'https:', 'mailto:'])
const SAFE_IMAGE_PROTOCOLS = new Set(['http:', 'https:'])

function resolveProtocol(url: string): string | undefined {
  try {
    return new URL(url, window.location.href).protocol
  } catch {
    return undefined
  }
}

export function safeHref(url: unknown): string | undefined {
  if (typeof url !== 'string' || url === '') return undefined
  const protocol = resolveProtocol(url)
  return protocol && SAFE_LINK_PROTOCOLS.has(protocol) ? url : undefined
}

export function safeImageSrc(url: unknown): string | undefined {
  if (typeof url !== 'string' || url === '') return undefined
  const protocol = resolveProtocol(url)
  if (!protocol) return undefined
  if (SAFE_IMAGE_PROTOCOLS.has(protocol)) return url
  return protocol === 'data:' && /^\s*data:image\//i.test(url) ? url : undefined
}
