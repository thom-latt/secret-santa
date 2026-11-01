import { i18n } from '../i18n'

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

// Known user-facing backend messages -> keys under `errors.` in the i18n files.
const knownErrors: Record<string, string> = {
  "event not found": 'event_not_found',
  "participant not found": 'participant_not_found',
  "participants not found": 'participant_not_found',
  "exclusion not found": 'exclusion_not_found',
  "wishlist item not found": 'wishlist_item_not_found',
  "draw has not taken place yet": 'draw_not_done',
  "this email is already in the participant list": 'email_duplicate',
  "cannot add participants after the draw": 'after_draw_add_participant',
  "cannot delete participants after the draw": 'after_draw_delete_participant',
  "cannot add exclusions after the draw": 'after_draw_add_exclusion',
  "cannot delete exclusions after the draw": 'after_draw_delete_exclusion',
  "the draw has already been executed": 'draw_already_done',
  "participants or exclusions kept changing during the draw, please retry": 'draw_conflict',
  "at least 3 participants are required to launch the draw": 'draw_too_few',
  "maximum of 100 participants reached": 'max_participants',
  "maximum of 50 wishlist items reached": 'max_wishlist',
  "this exclusion already exists": 'exclusion_duplicate',
  "two distinct participants must be selected": 'exclusion_distinct',
  "title, first_name, last_name, and a valid email are required": 'organizer_fields',
  "first_name, last_name, and a valid email are required": 'participant_fields',
  "invalid email address": 'invalid_email',
  "invalid event details (title is required, max 200 chars; description max 2000 chars)": 'invalid_event_details',
  "invalid event date (expected YYYY-MM-DD)": 'invalid_event_date',
  "title is required and must be at most 200 characters": 'wishlist_title',
  "note must be at most 1000 characters": 'wishlist_note',
  "invalid URL": 'invalid_url',
  "only http:// and https:// URLs are allowed": 'url_scheme',
  "public event creation is disabled on this instance": 'creation_disabled',
  "too many requests, please try again later": 'rate_limited',
  "too many events created from this address, please try again later": 'rate_limited',
  "request body too large": 'body_too_large',
  "invalid request body": 'invalid_body',
  "invalid feedback": 'invalid_feedback',
  "too many feedback reports, please try again later": 'feedback_rate_limited',
  "failed to send feedback": 'feedback_send_failed',
  "feedback is not available on this instance": 'feedback_unavailable'
}

/**
 * Turns a failed API response into a message in the UI language. Unknown or
 * missing messages fall back on the status code, so English server text and
 * raw 5xx details never reach the user.
 */
export function apiErrorMessage(res: Response, data: { error?: unknown }): string {
  const key =
    (typeof data.error === 'string' ? knownErrors[data.error] : undefined) ??
    (res.status === 429 ? 'rate_limited' : res.status >= 500 ? 'server' : 'generic')
  return i18n.global.t(`errors.${key}`)
}
