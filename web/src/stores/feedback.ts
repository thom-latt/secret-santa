import { apiErrorMessage, readJson } from './http'

export type FeedbackScope = 'admin' | 'participant'
export type FeedbackKind = 'bug' | 'feedback' | 'idea'

export async function sendFeedback(
  scope: FeedbackScope,
  token: string,
  payload: { kind: FeedbackKind; message: string },
): Promise<void> {
  const base = scope === 'admin' ? '/api/admin/events' : '/api/p'
  const res = await fetch(`${base}/${encodeURIComponent(token)}/feedback`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    throw new Error(apiErrorMessage(res, await readJson(res)))
  }
}
