/**
 * Returns the URL only if it is an absolute http(s) URL, otherwise undefined.
 * The API already rejects other schemes; this is defence in depth so that a
 * `javascript:` or `data:` URL can never end up in an href.
 */
export function safeHref(url: string | null | undefined): string | undefined {
  if (!url) return undefined
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? url : undefined
  } catch {
    return undefined
  }
}
