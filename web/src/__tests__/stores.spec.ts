import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAppConfigStore } from '../stores/appConfig'
import { useEventAdminStore } from '../stores/eventAdmin'
import { useParticipantStore } from '../stores/participant'
import { useSuperAdminStore } from '../stores/superAdmin'
import { readJson, apiErrorMessage } from '../stores/http'
import { setLocale } from '../i18n'
import { safeHref } from '../utils/safeUrl'

type Call = { url: string; init?: RequestInit }

let calls: Call[]
let queue: Array<Response | Error>

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}
function html(status: number): Response {
  return new Response('<html><body>Bad gateway</body></html>', { status, headers: { 'Content-Type': 'text/html' } })
}
function enqueue(...responses: Array<Response | Error>) {
  queue.push(...responses)
}
function lastCall(): Call {
  return calls[calls.length - 1]
}
function sentBody(call: Call = lastCall()): Record<string, unknown> {
  return JSON.parse(String(call.init?.body))
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  calls = []
  queue = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init })
      const next = queue.shift()
      if (!next) throw new Error(`unexpected fetch ${init?.method ?? 'GET'} ${url}`)
      if (next instanceof Error) throw next
      return next
    }),
  )
})
afterEach(() => {
  setLocale('en')
  vi.unstubAllGlobals()
})

describe('apiErrorMessage', () => {
  it('translates known backend messages into the UI language', () => {
    setLocale('fr')
    expect(apiErrorMessage(new Response(null, { status: 409 }), { error: 'this email is already in the participant list' })).toBe(
      'Cet email figure déjà dans la liste des participants.',
    )
    setLocale('en')
    expect(apiErrorMessage(new Response(null, { status: 404 }), { error: 'event not found' })).toBe('Event not found.')
    setLocale('fr')
    expect(apiErrorMessage(new Response(null, { status: 429 }), { error: 'too many requests, please try again later' })).toBe(
      'Trop de requêtes, veuillez réessayer plus tard.',
    )
  })

  it('falls back on the status for unknown or missing messages', () => {
    setLocale('en')
    expect(apiErrorMessage(new Response(null, { status: 500 }), { error: 'failed to load event' })).toBe(
      'Server error, please try again in a moment.',
    )
    expect(apiErrorMessage(new Response(null, { status: 502 }), {})).toBe('Server error, please try again in a moment.')
    expect(apiErrorMessage(new Response(null, { status: 429 }), { error: 'whatever' })).toBe(
      'Too many requests, please try again later.',
    )
    expect(apiErrorMessage(new Response(null, { status: 422 }), { error: 'Alice cannot offer to anyone' })).toBe(
      'Something went wrong, please try again.',
    )
  })
})

describe('readJson / safeHref helpers', () => {
  it('readJson tolerates empty and non-JSON bodies', async () => {
    expect(await readJson(json({ a: 1 }))).toEqual({ a: 1 })
    expect(await readJson(html(502))).toEqual({})
    expect(await readJson(new Response('', { status: 200 }))).toEqual({})
    expect(await readJson(new Response(null, { status: 204 }))).toEqual({})
  })

  it('safeHref only lets absolute http(s) URLs through', () => {
    expect(safeHref('https://example.com/a?b=c')).toBe('https://example.com/a?b=c')
    expect(safeHref('http://example.com')).toBe('http://example.com')
    for (const bad of [
      'javascript:alert(1)',
      'JaVaScRiPt:alert(1)',
      ' javascript:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      'vbscript:msgbox(1)',
      'file:///etc/passwd',
      'ftp://example.com',
      '//evil.example/x',
      'example.com',
      '',
      null,
      undefined,
    ]) {
      expect(safeHref(bad as string | null | undefined)).toBeUndefined()
    }
  })
})

describe('appConfig store', () => {
  it('loads the public configuration', async () => {
    const store = useAppConfigStore()
    enqueue(json({ allow_public_event_creation: false }))
    await store.fetchPublicConfig()
    expect(lastCall().url).toBe('/api/public/config')
    expect(store.allowPublicEventCreation).toBe(false)
    expect(store.loaded).toBe(true)
  })

  it('keeps defaults and reports an error when the configuration cannot be loaded', async () => {
    const store = useAppConfigStore()
    enqueue(json({}, 500))
    await expect(store.fetchPublicConfig()).rejects.toThrow('Server error')
    expect(store.loaded).toBe(false)
  })

  it('sends exactly the fields the API accepts when creating an event', async () => {
    const store = useAppConfigStore()
    enqueue(json({ id: 'e1', title: 'T', admin_token: 'tok' }, 201))
    const res = await store.createEvent({ title: 'T', first_name: 'A', last_name: 'B', email: 'a@b.co', locale: 'fr' })
    expect(res.admin_token).toBe('tok')
    expect(lastCall().url).toBe('/api/events')
    expect(lastCall().init?.method).toBe('POST')
    expect(Object.keys(sentBody()).sort()).toEqual(['email', 'first_name', 'last_name', 'locale', 'title'])
  })

  it('surfaces the server message, including rate limiting', async () => {
    const store = useAppConfigStore()
    enqueue(json({ error: 'too many events created from this address, please try again later' }, 429))
    await expect(
      store.createEvent({ title: 'T', first_name: 'A', last_name: 'B', email: 'a@b.co', locale: 'en' }),
    ).rejects.toThrow('Too many requests')
  })

  it('does not leak a JSON parse error when a proxy answers with an HTML page', async () => {
    const store = useAppConfigStore()
    enqueue(html(502))
    await expect(
      store.createEvent({ title: 'T', first_name: 'A', last_name: 'B', email: 'a@b.co', locale: 'en' }),
    ).rejects.toThrow('Server error')
    enqueue(html(413))
    await expect(store.recoverLinks('a@b.co', 'en')).rejects.toThrow('Something went wrong')
  })

  it('recovers links with email and locale only', async () => {
    const store = useAppConfigStore()
    enqueue(json({ message: 'sent' }))
    expect(await store.recoverLinks('a@b.co', 'en')).toBe('sent')
    expect(lastCall().url).toBe('/api/recover')
    expect(Object.keys(sentBody()).sort()).toEqual(['email', 'locale'])
  })
})

describe('eventAdmin store', () => {
  const eventData = { id: 'e1', title: 'T', participants: [], exclusions: [], status: 'draft' }

  it('loads an event and url-encodes the token', async () => {
    const store = useEventAdminStore()
    enqueue(json(eventData))
    await store.fetchEvent('a/b?c')
    expect(lastCall().url).toBe('/api/admin/events/a%2Fb%3Fc')
    expect(store.event?.id).toBe('e1')
    expect(store.notFound).toBe(false)
    expect(store.loading).toBe(false)
  })

  it('flags 4xx answers as not found and clears stale data', async () => {
    const store = useEventAdminStore()
    enqueue(json(eventData))
    await store.fetchEvent('t')
    for (const status of [404, 400, 429]) {
      enqueue(json({ error: 'nope' }, status))
      await store.fetchEvent('t')
      expect(store.notFound).toBe(true)
      expect(store.event).toBeNull()
    }
  })

  it('reports server and network failures as errors, not as "not found"', async () => {
    const store = useEventAdminStore()
    enqueue(json({ error: 'boom' }, 500))
    await store.fetchEvent('t')
    expect(store.error).toBe('Server error, please try again in a moment.')
    expect(store.notFound).toBe(false)

    enqueue(html(502))
    await store.fetchEvent('t')
    expect(store.error).toBe('Server error, please try again in a moment.')

    enqueue(new TypeError('Failed to fetch'))
    await store.fetchEvent('t')
    expect(store.error).toBe('Failed to fetch')
    expect(store.loading).toBe(false)
  })

  it('sends only API-known fields for details, participants and exclusions', async () => {
    const store = useEventAdminStore()
    enqueue(json(eventData))
    await store.updateDetails('tok', {
      title: 'T',
      description: 'D',
      event_date: '2026-12-24',
      budget: '20',
      organizer_participates: true,
    })
    expect(lastCall().init?.method).toBe('PUT')
    expect(Object.keys(sentBody()).sort()).toEqual(['budget', 'description', 'event_date', 'organizer_participates', 'title'])

    enqueue(json(eventData, 201))
    await store.addParticipant('tok', { first_name: 'A', last_name: 'B', email: 'a@b.co' })
    expect(lastCall().url).toBe('/api/admin/events/tok/participants')
    expect(Object.keys(sentBody()).sort()).toEqual(['email', 'first_name', 'last_name'])

    enqueue(json(eventData))
    await store.updateParticipant('tok', 'p/1', { first_name: 'A', last_name: 'B', email: 'a@b.co' })
    expect(lastCall().url).toBe('/api/admin/events/tok/participants/p%2F1')
    expect(lastCall().init?.method).toBe('PUT')
    expect(Object.keys(sentBody()).sort()).toEqual(['email', 'first_name', 'last_name'])
  })

  it('sends directional exclusions with an explicit reciprocal flag (false by default)', async () => {
    const store = useEventAdminStore()
    enqueue(json(eventData, 201))
    await store.addExclusion('tok', 'a', 'b')
    expect(lastCall().url).toBe('/api/admin/events/tok/exclusions')
    expect(sentBody()).toEqual({ participant_a_id: 'a', participant_b_id: 'b', reciprocal: false })

    enqueue(json(eventData, 201))
    await store.addExclusion('tok', 'a', 'b', true)
    expect(sentBody()).toEqual({ participant_a_id: 'a', participant_b_id: 'b', reciprocal: true })
  })

  it('performs the remaining admin actions on the right endpoints', async () => {
    const store = useEventAdminStore()
    const cases: Array<[() => Promise<void>, string, string]> = [
      [() => store.deleteParticipant('tok', 'p1'), 'DELETE', '/api/admin/events/tok/participants/p1'],
      [() => store.deleteExclusion('tok', 'x1'), 'DELETE', '/api/admin/events/tok/exclusions/x1'],
      [() => store.executeDraw('tok'), 'POST', '/api/admin/events/tok/draw'],
      [() => store.resendParticipantEmail('tok', 'p1'), 'POST', '/api/admin/events/tok/participants/p1/resend'],
      [() => store.resendFailedEmails('tok'), 'POST', '/api/admin/events/tok/resend-failed'],
    ]
    for (const [run, method, url] of cases) {
      enqueue(json({ ...eventData, id: url }))
      await run()
      expect(lastCall().init?.method).toBe(method)
      expect(lastCall().url).toBe(url)
      expect(store.event?.id).toBe(url) // state follows the server response
    }
    enqueue(json({ message: 'deleted' }))
    await store.deleteEvent('tok')
    expect(lastCall().init?.method).toBe('DELETE')
    expect(store.event).toBeNull()
  })

  it('throws the server message and keeps the previous state on failures', async () => {
    const store = useEventAdminStore()
    enqueue(json(eventData))
    await store.fetchEvent('tok')

    enqueue(json({ error: 'cannot add participants after the draw' }, 409))
    await expect(store.addParticipant('tok', { first_name: 'A', last_name: 'B', email: 'a@b.co' })).rejects.toThrow(
      'Participants cannot be added after the draw.',
    )
    enqueue(json({ error: 'participants or exclusions kept changing during the draw, please retry' }, 409))
    await expect(store.executeDraw('tok')).rejects.toThrow('changed during the draw')
    enqueue(json({ error: 'Alice cannot offer to anyone' }, 422))
    await expect(store.executeDraw('tok')).rejects.toThrow('Something went wrong')
    enqueue(html(504))
    await expect(store.executeDraw('tok')).rejects.toThrow('Server error')
    enqueue(html(502))
    await expect(store.addExclusion('tok', 'a', 'b')).rejects.toThrow('Server error')
    expect(store.event?.id).toBe('e1')
  })
})

describe('participant store', () => {
  const space = { first_name: 'A', last_name: 'B', revealed: false, own_wishlist: [] }

  it('loads the space, distinguishing not found from server errors', async () => {
    const store = useParticipantStore()
    enqueue(json(space))
    await store.fetchSpace('t/k')
    expect(lastCall().url).toBe('/api/p/t%2Fk')
    expect(store.data?.first_name).toBe('A')

    enqueue(json({ error: 'draw has not taken place yet' }, 404))
    await store.fetchSpace('t')
    expect(store.notFound).toBe(true)
    expect(store.data).toBeNull()

    enqueue(json({}, 500))
    await store.fetchSpace('t')
    expect(store.notFound).toBe(false)
    expect(store.error).toBe('Server error, please try again in a moment.')

    enqueue(new TypeError('network down'))
    await store.fetchSpace('t')
    expect(store.error).toBe('network down')
  })

  it('reveals the target and updates state from the response', async () => {
    const store = useParticipantStore()
    enqueue(json({ ...space, revealed: true, target: { first_name: 'T', last_name: 'G', wishlist: [] } }))
    await store.revealTarget('tok')
    expect(lastCall().url).toBe('/api/p/tok/reveal')
    expect(lastCall().init?.method).toBe('POST')
    expect(store.data?.target?.first_name).toBe('T')

    enqueue(json({ error: 'participant not found' }, 404))
    await expect(store.revealTarget('tok')).rejects.toThrow('Participant not found.')
  })

  it('sends only title, url and note for wishlist items', async () => {
    const store = useParticipantStore()
    enqueue(json(space, 201))
    await store.addWishlistItem('tok', { title: 'T', url: 'example.com', note: 'N' })
    expect(lastCall().url).toBe('/api/p/tok/wishlist')
    expect(Object.keys(sentBody()).sort()).toEqual(['note', 'title', 'url'])

    enqueue(json(space))
    await store.updateWishlistItem('tok', 'i/1', { title: 'T', url: '', note: '' })
    expect(lastCall().url).toBe('/api/p/tok/wishlist/i%2F1')
    expect(lastCall().init?.method).toBe('PUT')
    expect(Object.keys(sentBody()).sort()).toEqual(['note', 'title', 'url'])

    enqueue(json(space))
    await store.deleteWishlistItem('tok', 'i1')
    expect(lastCall().init?.method).toBe('DELETE')

    enqueue(json({ error: 'only http:// and https:// URLs are allowed' }, 400))
    await expect(store.addWishlistItem('tok', { title: 'T', url: 'javascript:1', note: '' })).rejects.toThrow(
      'Only http:// and https:// URLs are allowed.',
    )
    enqueue(html(502))
    await expect(store.deleteWishlistItem('tok', 'i1')).rejects.toThrow('Server error')
  })
})

describe('superAdmin store', () => {
  const overview = {
    allow_public_event_creation: false,
    super_admin_email: 'root@example.com',
    events: [{ id: 'e1', title: 'T' }],
  }

  it('treats 401 as "not logged in" without throwing', async () => {
    const store = useSuperAdminStore()
    store.authenticated = true
    enqueue(json({ error: 'unauthorized' }, 401))
    await store.fetchOverview()
    expect(store.authenticated).toBe(false)
    expect(store.loading).toBe(false)
  })

  it('loads the overview when authenticated and throws on server errors', async () => {
    const store = useSuperAdminStore()
    enqueue(json(overview))
    await store.fetchOverview()
    expect(store.authenticated).toBe(true)
    expect(store.allowPublicEventCreation).toBe(false)
    expect(store.superAdminEmail).toBe('root@example.com')
    expect(store.events).toHaveLength(1)

    enqueue(json({ error: 'failed to load events' }, 500))
    await expect(store.fetchOverview()).rejects.toThrow('failed to load events')
    expect(store.loading).toBe(false)
  })

  it('logs in with email and password only, then loads the overview', async () => {
    const store = useSuperAdminStore()
    enqueue(json({ message: 'authenticated' }), json(overview))
    await store.login('root@example.com', 'pw')
    expect(calls[0].url).toBe('/api/superadmin/login')
    expect(Object.keys(sentBody(calls[0])).sort()).toEqual(['email', 'password'])
    expect(calls[1].url).toBe('/api/superadmin/overview')
    expect(store.authenticated).toBe(true)
  })

  it('shows the lock-out message after too many failed logins', async () => {
    const store = useSuperAdminStore()
    enqueue(json({ error: 'too many login attempts, please try again later' }, 429))
    await expect(store.login('root@example.com', 'bad')).rejects.toThrow('too many login attempts')
    expect(store.authenticated).toBe(false)
    enqueue(html(502))
    await expect(store.login('root@example.com', 'bad')).rejects.toThrow('Invalid credentials')
  })

  it('logs out locally and remotely', async () => {
    const store = useSuperAdminStore()
    store.authenticated = true
    store.events = [{ id: 'e1' }] as never
    enqueue(json({ message: 'logged out' }))
    await store.logout()
    expect(lastCall().url).toBe('/api/superadmin/logout')
    expect(lastCall().init?.method).toBe('POST')
    expect(store.authenticated).toBe(false)
    expect(store.events).toEqual([])
  })

  it('sends the documented payloads for settings, invitations, SMTP test, resend and delete', async () => {
    const store = useSuperAdminStore()
    enqueue(json({ allow_public_event_creation: true }))
    await store.togglePublicCreation(true)
    expect(sentBody()).toEqual({ allow_public_event_creation: true })
    expect(store.allowPublicEventCreation).toBe(true)

    enqueue(json({ id: 'e2' }, 201), json(overview))
    await store.inviteOrganizer({ title: 'T', first_name: 'A', last_name: 'B', email: 'a@b.co', locale: 'fr' })
    expect(Object.keys(sentBody(calls[calls.length - 2])).sort()).toEqual(['email', 'first_name', 'last_name', 'locale', 'title'])
    expect(lastCall().url).toBe('/api/superadmin/overview') // list refreshed after inviting

    enqueue(json({ message: 'test email sent successfully' }))
    expect(await store.testSmtp()).toBe('test email sent successfully')

    enqueue(json({ message: 'admin link resent' }))
    await store.resendAdminLink('e/1')
    expect(lastCall().url).toBe('/api/superadmin/events/e%2F1/resend-admin')

    enqueue(json({ message: 'deleted' }), json(overview))
    await store.deleteEvent('e1')
    expect(calls[calls.length - 2].init?.method).toBe('DELETE')
    expect(lastCall().url).toBe('/api/superadmin/overview')
  })

  it('propagates server messages for failed back-office actions', async () => {
    const store = useSuperAdminStore()
    enqueue(json({ error: 'SMTP test failed: connection refused' }, 502))
    await expect(store.testSmtp()).rejects.toThrow('SMTP test failed: connection refused')
    enqueue(json({ error: 'event not found' }, 404))
    await expect(store.deleteEvent('x')).rejects.toThrow('event not found')
    enqueue(json({ error: 'failed to send email: boom' }, 502))
    await expect(store.resendAdminLink('x')).rejects.toThrow('failed to send email')
    enqueue(html(502))
    await expect(store.inviteOrganizer({ title: 'T', first_name: 'A', last_name: 'B', email: 'a@b.co', locale: 'en' })).rejects.toThrow(
      'Failed to invite organizer',
    )
    enqueue(html(502))
    await expect(store.togglePublicCreation(false)).rejects.toThrow('Failed to update setting')
  })
})
