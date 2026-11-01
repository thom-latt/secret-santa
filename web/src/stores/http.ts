/**
 * Reads a response body as JSON. Returns an empty object when the body is empty
 * or not JSON (e.g. an HTML error page from a reverse proxy: 502, 413...), so that
 * callers fall back to their own error message instead of surfacing a
 * "Unexpected token '<'" parse error to the user.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export async function readJson(res: Response): Promise<any> {
  try {
    return await res.json()
  } catch {
    return {}
  }
}
