import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n, setLocale } from '../i18n'
import HomeView from '../views/HomeView.vue'
import ParticipantView from '../views/ParticipantView.vue'
import SuperAdminView from '../views/SuperAdminView.vue'
import PrivacyView from '../views/PrivacyView.vue'
import { routes } from '../router'

// ------------------------------------------------------------------ fake backend

type Handler = (req: { url: string; method: string; body: Record<string, unknown> | null }) => Response | Promise<Response>
let handlers: Array<{ match: (m: string, u: string) => boolean; handle: Handler }>
let requests: Array<{ url: string; method: string; body: Record<string, unknown> | null }>

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}
function on(method: string, url: string | RegExp, handle: Handler) {
  handlers.unshift({
    match: (m, u) => m === method && (typeof url === 'string' ? u === url : url.test(u)),
    handle,
  })
}
function count(method: string, url: string | RegExp): number {
  return requests.filter((r) => r.method === method && (typeof url === 'string' ? r.url === url : url.test(r.url))).length
}

beforeEach(() => {
  setLocale('en')
  handlers = []
  requests = []
  setActivePinia(createPinia())
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null
      const req = { url, method, body }
      requests.push(req)
      const h = handlers.find((x) => x.match(method, url))
      if (!h) return json({ error: `no handler for ${method} ${url}` }, 500)
      return h.handle(req)
    }),
  )
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountAt(component: object, path: string): Promise<{ wrapper: VueWrapper; router: ReturnType<typeof createRouter> }> {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(component, { attachTo: document.body, global: { plugins: [createPinia(), router, i18n] } })
  await flushPromises()
  return { wrapper, router }
}

// ------------------------------------------------------------------ HomeView

describe('HomeView with the real store', () => {
  async function fillCreateForm(wrapper: VueWrapper) {
    await wrapper.get('#event-title').setValue('Xmas')
    await wrapper.get('#org-first-name').setValue('Alice')
    await wrapper.get('#org-last-name').setValue('Martin')
    await wrapper.get('#org-email').setValue('alice@example.com')
  }

  it('creates an event and navigates to its admin page', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true }))
    on('POST', '/api/events', () => json({ id: 'e1', title: 'Xmas', admin_token: 'adm tok' }, 201))
    const { wrapper, router } = await mountAt(HomeView, '/')
    await fillCreateForm(wrapper)
    await wrapper.get('[data-testid="create-event-form"]').trigger('submit.prevent')
    await flushPromises()

    expect(requests.find((r) => r.url === '/api/events')?.body).toEqual({
      title: 'Xmas',
      first_name: 'Alice',
      last_name: 'Martin',
      email: 'alice@example.com',
      locale: 'en',
    })
    expect(router.currentRoute.value.fullPath).toBe('/admin/events/adm%20tok')
  })

  it('shows the server error, stays on the page and re-enables the button', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true }))
    on('POST', '/api/events', () => json({ error: 'too many events created from this address, please try again later' }, 429))
    const { wrapper, router } = await mountAt(HomeView, '/')
    await fillCreateForm(wrapper)
    await wrapper.get('[data-testid="create-event-form"]').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('Too many requests')
    expect(router.currentRoute.value.path).toBe('/')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    expect((wrapper.get('#event-title').element as HTMLInputElement).value).toBe('Xmas') // input is kept
  })

  it('disables the submit button while the request is in flight (no double submit)', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true }))
    let release: (r: Response) => void = () => {}
    on('POST', '/api/events', () => new Promise<Response>((resolve) => (release = resolve)))
    const { wrapper } = await mountAt(HomeView, '/')
    await fillCreateForm(wrapper)
    await wrapper.get('[data-testid="create-event-form"]').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[type="submit"]').text()).toBe(i18n.global.t('home.creating'))
    release(json({ id: 'e1', title: 'Xmas', admin_token: 'tok' }, 201))
    await flushPromises()
    expect(count('POST', '/api/events')).toBe(1)
  })

  it('stays private (no creation form) when the public configuration cannot be loaded', async () => {
    on('GET', '/api/public/config', () => json({ error: 'x' }, 500))
    const { wrapper } = await mountAt(HomeView, '/')
    expect(wrapper.find('[data-testid="create-event-form"]').exists()).toBe(false)
    expect(wrapper.get('h1').text()).toBe(i18n.global.t('home.private_title'))
    expect(wrapper.get('[data-testid="public-disabled-banner"]').text()).toBe(i18n.global.t('home.private_banner'))
  })

  it('shows recovery errors and clears them on the next successful attempt', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true }))
    let fail = true
    on('POST', '/api/recover', () =>
      fail ? json({ error: 'too many requests, please try again later' }, 429) : json({ message: 'An email has been sent.' }),
    )
    const { wrapper } = await mountAt(HomeView, '/')
    const form = wrapper.get('[data-testid="recover-form"]')
    await form.get('input[type="email"]').setValue('a@b.co')
    await form.trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.text()).toContain('Too many requests')
    expect(wrapper.find('[data-testid="recover-message"]').exists()).toBe(false)

    fail = false
    await form.get('input[type="email"]').setValue('a@b.co')
    await form.trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.get('[data-testid="recover-message"]').text()).toBe('An email has been sent.')
    expect(wrapper.text()).not.toContain('Too many requests')
    expect((form.get('input[type="email"]').element as HTMLInputElement).value).toBe('')
  })
})

// ------------------------------------------------------------------ ParticipantView

describe('ParticipantView with the real store', () => {
  const baseSpace = {
    first_name: 'Alice',
    last_name: 'Martin',
    email: 'alice@example.com',
    event_title: 'Xmas',
    event_date: '',
    budget: '',
    description: '',
    locale: 'en',
    revealed: false,
    link_clicked_at: null,
    own_wishlist: [] as Array<Record<string, unknown>>,
  }
  const item = (over: Record<string, unknown> = {}) => ({ id: 'i1', title: 'Book', url: '', note: '', sort_order: 0, ...over })

  it('shows 404 for unknown links and a retryable error for server failures', async () => {
    on('GET', '/api/p/gone', () => json({ error: 'participant not found' }, 404))
    const missing = await mountAt(ParticipantView, '/p/gone')
    expect(missing.wrapper.find('[data-testid="not-found"]').exists()).toBe(true)

    let healthy = false
    on('GET', '/api/p/flaky', () => (healthy ? json(baseSpace) : json({ error: 'db down' }, 500)))
    const { wrapper } = await mountAt(ParticipantView, '/p/flaky')
    expect(wrapper.find('[data-testid="server-error"]').exists()).toBe(true)
    healthy = true
    await wrapper.get('[data-testid="error-retry-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="server-error"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Xmas')
  })

  it('keeps the target out of the DOM until reveal, then lets the user hide it again', async () => {
    on('GET', '/api/p/tok', () => json(baseSpace))
    on('POST', '/api/p/tok/reveal', () =>
      json({ ...baseSpace, revealed: true, target: { first_name: 'Bob', last_name: 'Durand', wishlist: [] } }),
    )
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    expect(wrapper.html()).not.toContain('Bob')
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(false)

    await wrapper.get('[data-testid="discover-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="target-full-name"]').text()).toBe('Bob Durand')
    expect(wrapper.find('[data-testid="target-wishlist-empty"]').exists()).toBe(true)

    await wrapper.get('[data-testid="toggle-visibility-btn"]').trigger('click')
    expect(wrapper.html()).not.toContain('Bob') // removed from the DOM, not merely hidden
    await wrapper.get('[data-testid="toggle-visibility-btn"]').trigger('click')
    expect(wrapper.html()).toContain('Bob')
  })

  it('shows an error and keeps the unrevealed state when the reveal request fails', async () => {
    on('GET', '/api/p/tok', () => json(baseSpace))
    on('POST', '/api/p/tok/reveal', () => json({ error: 'participant not found' }, 404))
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    await wrapper.get('[data-testid="discover-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Participant not found.')
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="discover-btn"]').exists()).toBe(true)
  })

  it('renders user-supplied text as text, never as HTML', async () => {
    const evil = '<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>'
    on('GET', '/api/p/tok', () =>
      json({
        ...baseSpace,
        event_title: evil,
        description: evil,
        revealed: true,
        target: {
          first_name: evil,
          last_name: '<b>bold</b>',
          wishlist: [item({ id: 't1', title: evil, note: evil })],
        },
        own_wishlist: [item({ title: evil, note: evil })],
      }),
    )
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('b').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src=x')
    expect((window as unknown as { __xss?: number }).__xss).toBeUndefined()
  })

  it('only turns http(s) URLs into links, and opens them safely', async () => {
    on('GET', '/api/p/tok', () =>
      json({
        ...baseSpace,
        revealed: true,
        target: {
          first_name: 'Bob',
          last_name: 'D',
          wishlist: [
            item({ id: 't1', title: 'ok', url: 'https://example.com/a' }),
            item({ id: 't2', title: 'evil', url: 'javascript:alert(1)' }),
            item({ id: 't3', title: 'evil2', url: 'data:text/html,<script>alert(1)</script>' }),
          ],
        },
        own_wishlist: [item({ url: 'JAVASCRIPT:alert(1)' })],
      }),
    )
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    const anchors = wrapper.findAll('a')
    expect(anchors).toHaveLength(4)
    const withHref = anchors.filter((a) => a.attributes('href'))
    expect(withHref).toHaveLength(1)
    expect(withHref[0].attributes('href')).toBe('https://example.com/a')
    expect(withHref[0].attributes('rel')).toContain('noopener')
    expect(withHref[0].attributes('rel')).toContain('noreferrer')
    expect(withHref[0].attributes('target')).toBe('_blank')
  })

  it('adds wishlist items: clears the form on success, keeps it and shows the reason on failure', async () => {
    let reject = true
    on('GET', '/api/p/tok', () => json(baseSpace))
    on('POST', '/api/p/tok/wishlist', (req) =>
      reject
        ? json({ error: 'only http:// and https:// URLs are allowed' }, 400)
        : json({ ...baseSpace, own_wishlist: [item({ title: String(req.body?.title) })] }, 201),
    )
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    await wrapper.get('[data-testid="new-wishlist-title"]').setValue('Book')
    await wrapper.get('[data-testid="new-wishlist-url"]').setValue('javascript:alert(1)')
    await wrapper.get('[data-testid="new-wishlist-note"]').setValue('hardcover')
    await wrapper.get('[data-testid="add-wishlist-form"]').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('Only http:// and https:// URLs are allowed.')
    expect((wrapper.get('[data-testid="new-wishlist-title"]').element as HTMLInputElement).value).toBe('Book')
    expect(wrapper.find('[data-testid="own-wishlist-empty"]').exists()).toBe(true)

    reject = false
    await wrapper.get('[data-testid="new-wishlist-url"]').setValue('example.com')
    await wrapper.get('[data-testid="add-wishlist-form"]').trigger('submit.prevent')
    await flushPromises()
    expect(requests.filter((r) => r.method === 'POST' && r.url === '/api/p/tok/wishlist').pop()?.body).toEqual({
      title: 'Book',
      url: 'example.com',
      note: 'hardcover',
    })
    expect(wrapper.find('[data-testid="own-wishlist"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="new-wishlist-title"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).not.toContain('only http:// and https://')
  })

  it('edits (with cancel) and deletes wishlist items', async () => {
    on('GET', '/api/p/tok', () => json({ ...baseSpace, own_wishlist: [item({ title: 'Book', url: 'https://a.example', note: 'n' })] }))
    on('PUT', '/api/p/tok/wishlist/i1', (req) => json({ ...baseSpace, own_wishlist: [item({ title: String(req.body?.title) })] }))
    on('DELETE', '/api/p/tok/wishlist/i1', () => json(baseSpace))
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')

    await wrapper.get('[data-testid="edit-wishlist-btn"]').trigger('click')
    expect((wrapper.get('[data-testid="edit-wishlist-title"]').element as HTMLInputElement).value).toBe('Book')
    expect((wrapper.get('[data-testid="edit-wishlist-url"]').element as HTMLInputElement).value).toBe('https://a.example')
    await wrapper.get('[data-testid="edit-wishlist-title"]').setValue('Novel')
    await wrapper.get('[data-testid="cancel-wishlist-btn"]').trigger('click')
    expect(wrapper.find('[data-testid="edit-wishlist-title"]').exists()).toBe(false)
    expect(count('PUT', '/api/p/tok/wishlist/i1')).toBe(0) // cancel sends nothing

    await wrapper.get('[data-testid="edit-wishlist-btn"]').trigger('click')
    await wrapper.get('[data-testid="edit-wishlist-title"]').setValue('Novel')
    await wrapper.get('[data-testid="save-wishlist-btn"]').trigger('click')
    await flushPromises()
    expect(requests.find((r) => r.method === 'PUT')?.body).toEqual({ title: 'Novel', url: 'https://a.example', note: 'n' })
    expect(wrapper.text()).toContain('Novel')
    expect(wrapper.find('[data-testid="edit-wishlist-title"]').exists()).toBe(false)

    await wrapper.get('[data-testid="delete-wishlist-btn"]').trigger('click')
    await flushPromises()
    expect(count('DELETE', '/api/p/tok/wishlist/i1')).toBe(1)
    expect(wrapper.find('[data-testid="own-wishlist-empty"]').exists()).toBe(true)
  })

  it('keeps the edit form open and shows the error when saving fails', async () => {
    on('GET', '/api/p/tok', () => json({ ...baseSpace, own_wishlist: [item()] }))
    on('PUT', '/api/p/tok/wishlist/i1', () => json({ error: 'wishlist item not found' }, 404))
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    await wrapper.get('[data-testid="edit-wishlist-btn"]').trigger('click')
    await wrapper.get('[data-testid="save-wishlist-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Wishlist item not found.')
    expect(wrapper.find('[data-testid="edit-wishlist-title"]').exists()).toBe(true)
  })
})

// ------------------------------------------------------------------ SuperAdminView

describe('SuperAdminView with the real store', () => {
  const ev = (over: Record<string, unknown> = {}) => ({
    id: 'e1',
    title: 'Xmas',
    organizer_first_name: 'Alice',
    organizer_last_name: 'Martin',
    organizer_email: 'alice@example.com',
    status: 'drawn',
    locale: 'en',
    created_at: '2026-10-25T12:00:00Z',
    drawn_at: null,
    participant_count: 4,
    revealed_targets_count: 1,
    filled_wishlists_count: 2,
    ...over,
  })
  let events: Array<ReturnType<typeof ev>>
  let allow: boolean

  function backend(authenticated = true) {
    events = [ev()]
    allow = true
    on('GET', '/api/superadmin/overview', () =>
      authenticated
        ? json({ allow_public_event_creation: allow, super_admin_email: 'root@example.com', events })
        : json({ error: 'unauthorized' }, 401),
    )
  }

  it('shows the login form when there is no session', async () => {
    backend(false)
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    expect(wrapper.find('[data-testid="superadmin-login-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="superadmin-events-list"]').exists()).toBe(false)
  })

  it('reports failed and rate-limited logins without leaving the login form', async () => {
    backend(false)
    let status = 401
    on('POST', '/api/superadmin/login', () =>
      status === 401 ? json({ error: 'invalid credentials' }, 401) : json({ error: 'too many login attempts, please try again later' }, 429),
    )
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    const form = wrapper.get('[data-testid="superadmin-login-form"]')
    await form.get('input[type="email"]').setValue('root@example.com')
    await form.get('input[type="password"]').setValue('wrong')
    await form.trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.text()).toContain('invalid credentials')

    status = 429
    await form.trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.text()).toContain('too many login attempts')
    expect(wrapper.find('[data-testid="superadmin-login-form"]').exists()).toBe(true)
  })

  it('logs in, lists events with their metrics, and logs out', async () => {
    backend(false)
    let loggedIn = false
    on('GET', '/api/superadmin/overview', () =>
      loggedIn
        ? json({ allow_public_event_creation: true, super_admin_email: 'root@example.com', events: [ev(), ev({ id: 'e2', title: 'Birthday', status: 'draft' })] })
        : json({ error: 'unauthorized' }, 401),
    )
    on('POST', '/api/superadmin/login', () => {
      loggedIn = true
      return json({ message: 'authenticated' })
    })
    on('POST', '/api/superadmin/logout', () => {
      loggedIn = false
      return json({ message: 'logged out' })
    })
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    const form = wrapper.get('[data-testid="superadmin-login-form"]')
    await form.get('input[type="email"]').setValue('root@example.com')
    await form.get('input[type="password"]').setValue('pw')
    await form.trigger('submit.prevent')
    await flushPromises()

    const cards = wrapper.findAll('[data-testid="superadmin-event-card"]')
    expect(cards).toHaveLength(2)
    expect(cards[0].get('[data-testid="event-revealed-count"]').text()).toContain('1/4')
    expect(cards[0].get('[data-testid="event-wishlists-count"]').text()).toContain('2/4')
    expect(cards[0].get('[data-testid="event-created-at"]').text()).toContain('2026-10-25')
    expect(wrapper.text()).toContain('root@example.com')

    await wrapper.get('[data-testid="superadmin-logout-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="superadmin-login-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="superadmin-event-card"]').exists()).toBe(false)
  })

  it('shows an empty state when no event exists', async () => {
    backend()
    events = []
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    expect(wrapper.find('[data-testid="superadmin-events-empty"]').exists()).toBe(true)
  })

  it('toggles public creation and reflects the saved value', async () => {
    backend()
    on('PUT', '/api/superadmin/settings', (req) => {
      allow = Boolean(req.body?.allow_public_event_creation)
      return json({ allow_public_event_creation: allow })
    })
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    const toggle = wrapper.get('[data-testid="toggle-public-creation"]')
    expect(toggle.text()).toBe('ON')
    await toggle.trigger('click')
    await flushPromises()
    expect(requests.find((r) => r.method === 'PUT')?.body).toEqual({ allow_public_event_creation: false })
    expect(toggle.text()).toBe('OFF')
    expect(toggle.attributes('aria-checked')).toBe('false')
  })

  it('invites an organizer in the UI language; keeps the form on failure, clears it on success', async () => {
    backend()
    let fail = true
    on('POST', '/api/superadmin/invite', () =>
      fail ? json({ error: 'event created, but failed to send email: smtp down' }, 502) : json({ id: 'e2', title: 'T', admin_token: 'x' }, 201),
    )
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    await wrapper.get('[data-testid="invite-title"]').setValue('Team party')
    await wrapper.get('[data-testid="invite-first-name"]').setValue('Bob')
    await wrapper.get('[data-testid="invite-last-name"]').setValue('Durand')
    await wrapper.get('[data-testid="invite-email"]').setValue('bob@example.com')
    await wrapper.get('[data-testid="invite-organizer-form"]').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.get('[data-testid="superadmin-action-error"]').text()).toContain('smtp down')
    expect((wrapper.get('[data-testid="invite-email"]').element as HTMLInputElement).value).toBe('bob@example.com')
    expect(requests.find((r) => r.url === '/api/superadmin/invite')?.body).toEqual({
      title: 'Team party',
      first_name: 'Bob',
      last_name: 'Durand',
      email: 'bob@example.com',
      locale: 'en',
    })

    fail = false
    await wrapper.get('[data-testid="invite-locale"]').setValue('fr')
    await wrapper.get('[data-testid="invite-organizer-form"]').trigger('submit.prevent')
    await flushPromises()
    expect(requests.filter((r) => r.url === '/api/superadmin/invite').pop()?.body?.locale).toBe('fr')
    expect(wrapper.get('[data-testid="invite-link"]').text()).toBe(`${window.location.origin}/admin/events/x`)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await wrapper.get('[data-testid="invite-copy-btn"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/admin/events/x`)
    expect(wrapper.find('[data-testid="superadmin-status-message"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="invite-email"]').element as HTMLInputElement).value).toBe('')
  })

  it('reports SMTP test results and admin-link resends, success and failure', async () => {
    backend()
    let smtpOk = false
    on('POST', '/api/superadmin/test-smtp', () => (smtpOk ? json({ message: 'test email sent successfully' }) : json({ error: 'SMTP test failed: connection refused' }, 502)))
    on('POST', '/api/superadmin/events/e1/resend-admin', () => json({ message: 'admin link resent' }))
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')

    await wrapper.get('[data-testid="test-smtp-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="superadmin-action-error"]').text()).toContain('connection refused')

    smtpOk = true
    await wrapper.get('[data-testid="test-smtp-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="superadmin-status-message"]').text()).toContain('test email sent successfully')
    expect(wrapper.find('[data-testid="superadmin-action-error"]').exists()).toBe(false) // stale error cleared

    await wrapper.get('[data-testid="resend-admin-link-btn"]').trigger('click')
    await flushPromises()
    expect(count('POST', '/api/superadmin/events/e1/resend-admin')).toBe(1)
    expect(wrapper.find('[data-testid="superadmin-status-message"]').exists()).toBe(true)
  })

  it('asks for confirmation before deleting, and only deletes after confirming', async () => {
    backend()
    on('DELETE', '/api/superadmin/events/e1', () => {
      events = []
      return json({ message: 'deleted' })
    })
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')

    await wrapper.get('[data-testid="superadmin-delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="superadmin-cancel-delete-btn"]').trigger('click')
    await flushPromises()
    expect(count('DELETE', '/api/superadmin/events/e1')).toBe(0)
    expect(wrapper.findAll('[data-testid="superadmin-event-card"]')).toHaveLength(1)

    await wrapper.get('[data-testid="superadmin-delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="superadmin-confirm-delete-btn"]').trigger('click')
    await flushPromises()
    expect(count('DELETE', '/api/superadmin/events/e1')).toBe(1)
    expect(wrapper.find('[data-testid="superadmin-events-empty"]').exists()).toBe(true)
  })

  it('shows the error and keeps the event when deletion fails', async () => {
    backend()
    on('DELETE', '/api/superadmin/events/e1', () => json({ error: 'failed to delete event' }, 500))
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    await wrapper.get('[data-testid="superadmin-delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="superadmin-confirm-delete-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="superadmin-action-error"]').text()).toContain('failed to delete event')
    expect(wrapper.findAll('[data-testid="superadmin-event-card"]')).toHaveLength(1)
  })

  it('falls back to the login form if the session expires mid-action', async () => {
    backend()
    on('DELETE', '/api/superadmin/events/e1', () => json({ message: 'deleted' }))
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    // session now expired: the refresh after deleting answers 401
    on('GET', '/api/superadmin/overview', () => json({ error: 'unauthorized' }, 401))
    await wrapper.get('[data-testid="superadmin-delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="superadmin-confirm-delete-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="superadmin-login-form"]').exists()).toBe(true)
  })
})

// ---------------------------------------------------------------- PrivacyView

describe('PrivacyView', () => {
  beforeEach(() => setLocale('en'))

  it('names the operator, states the retention periods and is routed at /privacy', async () => {
    on('GET', '/api/public/config', () =>
      json({ allow_public_event_creation: false, operator_name: 'Jane Doe', operator_email: 'jane@example.org' }),
    )
    const { wrapper, router } = await mountAt(PrivacyView, '/privacy')
    expect(router.currentRoute.value.name).toBe('privacy')
    const text = wrapper.text()
    expect(text).toContain('Jane Doe')
    expect(text).toContain('30 days')
    expect(text).toContain('12 months')
    expect(wrapper.get('a[href="mailto:jane@example.org"]').text()).toBe('jane@example.org')
  })

  it('renders in French', async () => {
    setLocale('fr')
    on('GET', '/api/public/config', () =>
      json({ allow_public_event_creation: false, operator_name: 'Jane Doe', operator_email: 'jane@example.org' }),
    )
    const { wrapper } = await mountAt(PrivacyView, '/privacy')
    expect(wrapper.text()).toContain('CNIL')
    expect(wrapper.text()).toContain('30 jours')
    expect(wrapper.text()).toContain('12 mois')
  })
})

// ------------------------------------------------------------------ Phase 9: accessibility & UX

describe('Accessibility and UX blockers', () => {
  const space = {
    first_name: 'Alice', last_name: 'Martin', email: 'alice@example.com', event_title: 'Xmas',
    event_date: '', budget: '', description: '', locale: 'en', revealed: false,
    link_clicked_at: null, own_wishlist: [] as Array<Record<string, unknown>>,
  }

  it('labels the recovery email and keeps focus rings on the home inputs', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true, operator_name: 'Op', operator_email: 'op@example.com' }))
    const { wrapper } = await mountAt(HomeView, '/')
    const input = wrapper.get('[data-testid="recover-form"] input')
    const label = wrapper.find(`label[for="${input.attributes('id')}"]`)
    expect(input.attributes('id')).toBeTruthy()
    expect(label.exists()).toBe(true)
    expect(wrapper.html()).not.toContain('focus:outline-hidden')
  })

  it('announces home errors with role="alert"', async () => {
    on('GET', '/api/public/config', () => json({ allow_public_event_creation: true, operator_name: 'Op', operator_email: 'op@example.com' }))
    on('POST', '/api/events', () => json({ error: 'invalid email address' }, 400))
    on('POST', '/api/recover', () => json({ error: 'nope' }, 400))
    const { wrapper } = await mountAt(HomeView, '/')
    await wrapper.get('#event-title').setValue('Xmas')
    await wrapper.get('#org-first-name').setValue('A')
    await wrapper.get('#org-last-name').setValue('B')
    await wrapper.get('#org-email').setValue('a@example.com')
    await wrapper.get('[data-testid="create-event-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Invalid email address.')
  })

  it('shows the feedback trigger only when the operator email is configured', async () => {
    const space = { first_name: 'Alice', last_name: 'M', email: 'a@example.com', event_title: 'X', event_date: '', budget: '', description: '', locale: 'en', revealed: false, link_clicked_at: null, own_wishlist: [] }
    on('GET', '/api/p/tok', () => json(space))
    on('GET', '/api/public/config', () => json({ operator_email: '' }))
    const without = await mountAt(ParticipantView, '/p/tok')
    expect(without.wrapper.find('[data-testid="feedback-open-btn"]').exists()).toBe(false)
    on('GET', '/api/public/config', () => json({ operator_email: 'op@example.com' }))
    const withEmail = await mountAt(ParticipantView, '/p/tok')
    expect(withEmail.wrapper.find('[data-testid="feedback-open-btn"]').exists()).toBe(true)
  })

  it('offers "Resend my links" on a 404 and shows a loading state on the participant page', async () => {
    on('GET', '/api/p/gone', () => json({ error: 'participant not found' }, 404))
    const { wrapper } = await mountAt(ParticipantView, '/p/gone')
    const link = wrapper.get('[data-testid="resend-links-link"]')
    expect(link.attributes('href')).toBe('/#recover')
    expect(link.text()).toBe('Resend my links')

    let release!: () => void
    on('GET', '/api/p/slow', () => new Promise<Response>((r) => { release = () => r(json(space)) }))
    const slow = await mountAt(ParticipantView, '/p/slow')
    expect(slow.wrapper.text()).toContain('Loading...')
    release()
    await flushPromises()
    expect(slow.wrapper.text()).not.toContain('Loading...')
  })

  it('announces participant errors and disables the reveal button while in flight', async () => {
    on('GET', '/api/p/tok', () => json(space))
    let fail!: () => void
    on('POST', '/api/p/tok/reveal', () => new Promise<Response>((r) => { fail = () => r(json({ error: 'participant not found' }, 404)) }))
    const { wrapper } = await mountAt(ParticipantView, '/p/tok')
    await wrapper.get('[data-testid="discover-btn"]').trigger('click')
    expect(wrapper.get('[data-testid="discover-btn"]').attributes('disabled')).toBeDefined()
    expect(count('POST', '/api/p/tok/reveal')).toBe(1)
    await wrapper.get('[data-testid="discover-btn"]').trigger('click')
    expect(count('POST', '/api/p/tok/reveal')).toBe(1)
    fail()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Participant not found.')
    expect(wrapper.get('[data-testid="discover-btn"]').attributes('disabled')).toBeUndefined()
  })

  it('associates labels with the super-admin login inputs', async () => {
    on('GET', '/api/superadmin/overview', () => json({ error: 'unauthorized' }, 401))
    const { wrapper } = await mountAt(SuperAdminView, '/superadmin')
    for (const input of wrapper.get('[data-testid="superadmin-login-form"]').findAll('input')) {
      expect(wrapper.find(`label[for="${input.attributes('id')}"]`).exists()).toBe(true)
    }
  })
})
