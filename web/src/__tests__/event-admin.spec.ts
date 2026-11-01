import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n, setLocale } from '../i18n'
import EventAdminView from '../views/EventAdminView.vue'
import { routes } from '../router'

// ------------------------------------------------------------------ in-memory fake backend

interface Part {
  id: string
  first_name: string
  last_name: string
  email: string
  is_organizer: boolean
  link_clicked_at: string | null
  email_sent_at: string | null
  email_error: string | null
  wishlist_count: number
}
interface Ex {
  id: string
  participant_a_id: string
  participant_b_id: string
}

const people: Array<[string, string, string]> = [
  ['p1', 'Alice', 'Martin'],
  ['p2', 'Bob', 'Durand'],
  ['p3', 'Carol', 'Petit'],
  ['p4', 'Dan', 'Roux'],
]

let status: 'draft' | 'drawn'
let exclusions: Ex[]
let nextId: number
let requests: Array<{ url: string; method: string; body: Record<string, unknown> | null }>
let overrides: Map<string, () => Response | Promise<Response>>

function json(body: unknown, code = 200): Response {
  return new Response(JSON.stringify(body), { status: code, headers: { 'Content-Type': 'application/json' } })
}

function eventPayload() {
  const participants: Part[] = people.map(([id, first, last], i) => ({
    id,
    first_name: first,
    last_name: last,
    email: `${first.toLowerCase()}@example.com`,
    is_organizer: i === 0,
    link_clicked_at: null,
    email_sent_at: status === 'drawn' ? '2026-11-01T10:00:00Z' : null,
    email_error: null,
    wishlist_count: 0,
  }))
  return {
    id: 'ev1',
    title: 'Xmas',
    description: '',
    event_date: '',
    budget: '',
    organizer_first_name: 'Alice',
    organizer_last_name: 'Martin',
    organizer_email: 'alice@example.com',
    organizer_participates: true,
    status,
    locale: 'en',
    created_at: '2026-10-25T12:00:00Z',
    drawn_at: null,
    participants,
    exclusions,
    feasibility: { mode: 'single_cycle', diagnostic: '' },
  }
}

beforeEach(() => {
  setLocale('en')
  status = 'draft'
  exclusions = []
  nextId = 1
  requests = []
  overrides = new Map()
  setActivePinia(createPinia())
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null
      requests.push({ url, method, body })
      const key = `${method} ${url}`
      const override = overrides.get(key)
      if (override) return override()

      if (key === 'GET /api/admin/events/tok') return json(eventPayload())
      if (key === 'PUT /api/admin/events/tok') return json(eventPayload())
      if (key === 'POST /api/admin/events/tok/exclusions') {
        const a = String(body?.participant_a_id)
        const b = String(body?.participant_b_id)
        const has = (x: string, y: string) => exclusions.some((e) => e.participant_a_id === x && e.participant_b_id === y)
        const pairs: Array<[string, string]> = [[a, b]]
        if (body?.reciprocal) pairs.push([b, a])
        const fresh = pairs.filter(([x, y]) => !has(x, y))
        if (fresh.length === 0) return json({ error: 'this exclusion already exists' }, 409)
        for (const [x, y] of fresh) exclusions.push({ id: `x${nextId++}`, participant_a_id: x, participant_b_id: y })
        return json(eventPayload(), 201)
      }
      const del = url.match(/^\/api\/admin\/events\/tok\/exclusions\/(.+)$/)
      if (method === 'DELETE' && del) {
        exclusions = exclusions.filter((e) => e.id !== del[1])
        return json(eventPayload())
      }
      if (key === 'POST /api/admin/events/tok/draw') {
        status = 'drawn'
        return json(eventPayload())
      }
      return json({ error: `unexpected ${key}` }, 500)
    }),
  )
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountAdmin(): Promise<VueWrapper> {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/admin/events/tok')
  await router.isReady()
  const wrapper = mount(EventAdminView, { attachTo: document.body, global: { plugins: [createPinia(), router, i18n] } })
  await flushPromises()
  return wrapper
}

const rowTexts = (w: VueWrapper) => w.findAll('[data-testid="exclusion-row"]').map((r) => r.find('span > span').text())
const addButton = (w: VueWrapper) => w.get('[data-testid="add-exclusion-btn"]')
const reciprocal = (w: VueWrapper) => w.get('[data-testid="exclusion-reciprocal"]')

async function choose(w: VueWrapper, giver: string, receiver: string) {
  await w.get('[data-testid="exclusion-select-a"]').setValue(giver)
  await w.get('[data-testid="exclusion-select-b"]').setValue(receiver)
}
async function submit(w: VueWrapper) {
  await w.get('[data-testid="add-exclusion-form"]').trigger('submit.prevent')
  await flushPromises()
}

// ------------------------------------------------------------------ directional exclusions

describe('EventAdminView - directional exclusions', () => {
  it('creates a one-way exclusion by default and resets the form afterwards', async () => {
    const w = await mountAdmin()
    expect((reciprocal(w).element as HTMLInputElement).checked).toBe(false)
    await choose(w, 'p1', 'p2')
    await submit(w)

    expect(requests.find((r) => r.method === 'POST')?.body).toEqual({ participant_a_id: 'p1', participant_b_id: 'p2', reciprocal: false })
    expect(rowTexts(w)).toEqual(["Alice Martin won't give to Bob Durand"])
    expect((w.get('[data-testid="exclusion-select-a"]').element as HTMLSelectElement).value).toBe('')
    expect((w.get('[data-testid="exclusion-select-b"]').element as HTMLSelectElement).value).toBe('')
    expect((reciprocal(w).element as HTMLInputElement).checked).toBe(false)
  })

  it('creates both directions in one click when "the other way round" is ticked', async () => {
    const w = await mountAdmin()
    await choose(w, 'p1', 'p2')
    await reciprocal(w).setValue(true)
    await submit(w)

    expect(requests.find((r) => r.method === 'POST')?.body).toEqual({ participant_a_id: 'p1', participant_b_id: 'p2', reciprocal: true })
    expect(rowTexts(w)).toEqual(["Alice Martin won't give to Bob Durand", "Bob Durand won't give to Alice Martin"])
    expect((reciprocal(w).element as HTMLInputElement).checked).toBe(false) // reset for the next one
  })

  it('names the people in the checkbox label once both are chosen', async () => {
    const w = await mountAdmin()
    const label = () => reciprocal(w).element.closest('label')!.textContent!.trim()
    expect(label()).toBe('Also apply the other way round (e.g. a couple)')
    await choose(w, 'p1', 'p2')
    expect(label()).toBe('Also prevent Bob Durand from offering to Alice Martin')
    await choose(w, 'p3', 'p2')
    expect(label()).toBe('Also prevent Bob Durand from offering to Carol Petit')
  })

  it('never lets someone be excluded from themselves', async () => {
    const w = await mountAdmin()
    await w.get('[data-testid="exclusion-select-a"]').setValue('p1')
    const optionsB = w.get('[data-testid="exclusion-select-b"]').findAll('option')
    // [0] placeholder, then p1..p4
    expect(optionsB[1].attributes('disabled')).toBeDefined()
    expect(optionsB[2].attributes('disabled')).toBeUndefined()
    await w.get('[data-testid="exclusion-select-b"]').setValue('p2')
    const optionsA = w.get('[data-testid="exclusion-select-a"]').findAll('option')
    expect(optionsA[2].attributes('disabled')).toBeDefined()
    expect(addButton(w).attributes('disabled')).toBeUndefined()
  })

  it('disables submit until both people are chosen', async () => {
    const w = await mountAdmin()
    expect(addButton(w).attributes('disabled')).toBeDefined()
    await w.get('[data-testid="exclusion-select-a"]').setValue('p1')
    expect(addButton(w).attributes('disabled')).toBeDefined()
    await w.get('[data-testid="exclusion-select-b"]').setValue('p2')
    expect(addButton(w).attributes('disabled')).toBeUndefined()
  })

  it('refuses an exact duplicate, explains why, and still allows adding the missing opposite direction', async () => {
    exclusions = [{ id: 'x0', participant_a_id: 'p1', participant_b_id: 'p2' }]
    const w = await mountAdmin()
    await choose(w, 'p1', 'p2')
    expect(addButton(w).attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="exclusion-exists-msg"]').text()).toBe('This exclusion already exists.')

    await reciprocal(w).setValue(true) // B -/-> A is still missing
    expect(addButton(w).attributes('disabled')).toBeUndefined()
    expect(w.find('[data-testid="exclusion-exists-msg"]').exists()).toBe(false)
    await submit(w)
    expect(rowTexts(w)).toEqual(["Alice Martin won't give to Bob Durand", "Bob Durand won't give to Alice Martin"])
  })

  it('treats the opposite direction as a different exclusion, and disables the checkbox when it already exists', async () => {
    exclusions = [{ id: 'x0', participant_a_id: 'p2', participant_b_id: 'p1' }]
    const w = await mountAdmin()
    await choose(w, 'p1', 'p2') // A -/-> B does not exist yet
    expect(addButton(w).attributes('disabled')).toBeUndefined()
    expect(w.find('[data-testid="exclusion-exists-msg"]').exists()).toBe(false)
    // B -/-> A exists already: the checkbox has nothing left to add
    expect(reciprocal(w).attributes('disabled')).toBeDefined()
    expect(reciprocal(w).element.closest('label')!.textContent).toContain('(already set)')
    await submit(w)
    expect(requests.find((r) => r.method === 'POST')?.body?.reciprocal).toBe(false)

    // everything exists now: nothing to add
    await choose(w, 'p1', 'p2')
    expect(addButton(w).attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="exclusion-exists-msg"]').exists()).toBe(true)
  })

  it('lists mutual pairs side by side, tags them "Mutual", and leaves one-way rows untagged', async () => {
    exclusions = [
      { id: 'x1', participant_a_id: 'p4', participant_b_id: 'p3' },
      { id: 'x2', participant_a_id: 'p1', participant_b_id: 'p3' }, // one way
      { id: 'x3', participant_a_id: 'p2', participant_b_id: 'p1' },
      { id: 'x4', participant_a_id: 'p3', participant_b_id: 'p4' },
      { id: 'x5', participant_a_id: 'p1', participant_b_id: 'p2' },
    ]
    const w = await mountAdmin()
    expect(rowTexts(w)).toEqual([
      "Alice Martin won't give to Bob Durand",
      "Bob Durand won't give to Alice Martin",
      "Alice Martin won't give to Carol Petit",
      "Carol Petit won't give to Dan Roux",
      "Dan Roux won't give to Carol Petit",
    ])
    const tagged = w.findAll('[data-testid="exclusion-row"]').map((r) => r.text().includes('Mutual'))
    expect(tagged).toEqual([true, true, false, true, true])
  })

  it('deletes a single direction and keeps the opposite one (which loses its "Mutual" tag)', async () => {
    exclusions = [
      { id: 'x1', participant_a_id: 'p1', participant_b_id: 'p2' },
      { id: 'x2', participant_a_id: 'p2', participant_b_id: 'p1' },
    ]
    const w = await mountAdmin()
    const buttons = w.findAll('[data-testid="exclusion-row"] button')
    expect(buttons[0].attributes('aria-label')).toBe("Delete : Alice Martin won't give to Bob Durand")
    await buttons[0].trigger('click')
    await flushPromises()

    expect(requests.some((r) => r.method === 'DELETE' && r.url.endsWith('/exclusions/x1'))).toBe(true)
    expect(requests.some((r) => r.method === 'DELETE' && r.url.endsWith('/exclusions/x2'))).toBe(false)
    expect(rowTexts(w)).toEqual(["Bob Durand won't give to Alice Martin"])
    expect(w.get('[data-testid="exclusion-row"]').text()).not.toContain('Mutual')
  })

  it('shows the server error and keeps the selection when adding fails', async () => {
    overrides.set('POST /api/admin/events/tok/exclusions', () => json({ error: 'cannot add exclusions after the draw' }, 409))
    const w = await mountAdmin()
    await choose(w, 'p1', 'p2')
    await submit(w)
    expect(w.text()).toContain('cannot add exclusions after the draw')
    expect((w.get('[data-testid="exclusion-select-a"]').element as HTMLSelectElement).value).toBe('p1')
    expect(rowTexts(w)).toEqual([])
  })

  it('exposes labelled controls and a live feasibility region', async () => {
    const w = await mountAdmin()
    for (const id of ['exclusion-select-a', 'exclusion-select-b']) {
      expect(w.find(`label[for="${id}"]`).exists()).toBe(true)
      expect(w.get(`#${id}`).attributes('data-testid')).toBe(id)
    }
    expect(w.get('[data-testid="feasibility-badge"]').attributes('aria-live')).toBe('polite')
    expect(w.find('ul[role="list"]').exists()).toBe(false) // the list only exists once there are rows
    await choose(w, 'p1', 'p2')
    await submit(w)
    expect(w.find('ul[role="list"]').exists()).toBe(true)
  })

  it('is not shown once the event is drawn', async () => {
    status = 'drawn'
    const w = await mountAdmin()
    expect(w.find('[data-testid="add-exclusion-form"]').exists()).toBe(false)
  })
})

// ------------------------------------------------------------------ draw flow

describe('EventAdminView - launching the draw', () => {
  async function launch(w: VueWrapper) {
    await w.get('[data-testid="launch-draw-btn"]').trigger('click')
    await w.get('[data-testid="confirm-draw-btn"]').trigger('click')
    await flushPromises()
  }

  it('saves the pending details, runs the draw and locks the editing sections', async () => {
    const w = await mountAdmin()
    await launch(w)
    const order = requests.map((r) => `${r.method} ${r.url}`)
    expect(order.indexOf('PUT /api/admin/events/tok')).toBeGreaterThan(-1)
    expect(order.indexOf('POST /api/admin/events/tok/draw')).toBeGreaterThan(order.indexOf('PUT /api/admin/events/tok'))
    expect(w.find('[data-testid="add-exclusion-form"]').exists()).toBe(false)
    expect(w.find('[data-testid="add-participant-form"]').exists()).toBe(false)
    expect(w.find('[data-testid="launch-draw-btn"]').exists()).toBe(false)
  })

  it.each([
    [422, 'Alice Martin cannot offer to anyone'],
    [409, 'participants or exclusions kept changing during the draw, please retry'],
    [409, 'the draw has already been executed'],
  ])('shows the reason when the draw is refused (%i: %s) and stays in draft', async (code, message) => {
    overrides.set('POST /api/admin/events/tok/draw', () => json({ error: message }, code))
    const w = await mountAdmin()
    await launch(w)
    expect(w.text()).toContain(message)
    expect(w.find('[data-testid="add-exclusion-form"]').exists()).toBe(true)
    expect(w.find('[data-testid="launch-draw-btn"]').exists()).toBe(true)
  })

  it('survives a proxy answering with an HTML page instead of JSON', async () => {
    overrides.set('POST /api/admin/events/tok/draw', () => new Response('<html>504 Gateway Time-out</html>', { status: 504 }))
    const w = await mountAdmin()
    await launch(w)
    expect(w.text()).toContain('Failed to execute draw')
    expect(w.text()).not.toContain('Unexpected token')
  })

  it('does not fire two draws when the confirm button is clicked twice', async () => {
    let release: (r: Response) => void = () => {}
    overrides.set('POST /api/admin/events/tok/draw', () => new Promise<Response>((resolve) => (release = resolve)))
    const w = await mountAdmin()
    await w.get('[data-testid="launch-draw-btn"]').trigger('click')
    await w.get('[data-testid="confirm-draw-btn"]').trigger('click')
    await w.get('[data-testid="confirm-draw-btn"]').trigger('click')
    await flushPromises()
    release(json(eventPayload()))
    await flushPromises()
    expect(requests.filter((r) => r.url.endsWith('/draw')).length).toBe(1)
  })
})
