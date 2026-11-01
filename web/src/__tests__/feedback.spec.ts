import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { i18n, setLocale } from '../i18n'
import FeedbackDialog from '../components/FeedbackDialog.vue'

let requests: Array<{ url: string; method: string; body: Record<string, unknown> | null }>
let operatorEmail: string
let feedbackReply: () => Response | Promise<Response>

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

beforeEach(() => {
  setLocale('en')
  requests = []
  operatorEmail = 'op@example.com'
  feedbackReply = () => new Response(null, { status: 204 })
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null
      requests.push({ url, method, body })
      if (url === '/api/public/config') return json({ allow_public_event_creation: false, operator_name: 'Op', operator_email: operatorEmail })
      if (url.endsWith('/feedback')) return feedbackReply()
      return json({ error: 'unexpected' }, 500)
    }),
  )
})
afterEach(() => vi.unstubAllGlobals())

async function mountDialog(scope: 'admin' | 'participant' = 'participant', token = 'tok'): Promise<VueWrapper> {
  const w = mount(FeedbackDialog, { attachTo: document.body, props: { scope, token }, global: { plugins: [createPinia(), i18n] } })
  await flushPromises()
  return w
}
const form = (w: VueWrapper) => w.get('[data-testid="feedback-form"]')
const submitBtn = (w: VueWrapper) => w.get('button[type="submit"][form]')

describe('FeedbackDialog', () => {
  it('renders nothing when the operator email is not configured', async () => {
    operatorEmail = ''
    const w = await mountDialog()
    expect(w.find('[data-testid="feedback-open-btn"]').exists()).toBe(false)
    expect(w.find('dialog').exists()).toBe(false)
  })

  it('labels its fields and explains that replies go to the email on record', async () => {
    const w = await mountDialog()
    for (const el of w.findAll('select, textarea')) {
      expect(w.find(`label[for="${el.attributes('id')}"]`).exists(), el.html()).toBe(true)
    }
    expect(w.text()).toContain('address on record')
  })

  it('rejects an empty message without calling the API', async () => {
    const w = await mountDialog()
    await form(w).trigger('submit')
    await flushPromises()
    expect(requests.filter((r) => r.url.endsWith('/feedback'))).toHaveLength(0)
    expect(w.get('[role="alert"]').text()).not.toBe('')
  })

  it('sends kind and message to the participant endpoint, then confirms and closes', async () => {
    const w = await mountDialog('participant', 'ptok')
    await w.get('[data-testid="feedback-open-btn"]').trigger('click')
    await w.get('dialog select').setValue('idea')
    await w.get('dialog textarea').setValue('  Add dark mode  ')
    expect(w.text()).toContain('2000')
    await form(w).trigger('submit')
    await flushPromises()
    const req = requests.find((r) => r.url.endsWith('/feedback'))!
    expect(req.url).toBe('/api/p/ptok/feedback')
    expect(req.method).toBe('POST')
    expect(req.body).toEqual({ kind: 'idea', message: 'Add dark mode' })
    expect(w.get('[data-testid="feedback-success"]').text()).toContain('Thank you')
    expect(w.get('dialog').element.hasAttribute('open')).toBe(false)
  })

  it('uses the admin endpoint for the organizer', async () => {
    const w = await mountDialog('admin', 'atok')
    await w.get('dialog textarea').setValue('bug here')
    await form(w).trigger('submit')
    await flushPromises()
    expect(requests.find((r) => r.url.endsWith('/feedback'))!.url).toBe('/api/admin/events/atok/feedback')
  })

  it('disables submit while sending and sends only once', async () => {
    let finish!: () => void
    feedbackReply = () => new Promise<Response>((r) => (finish = () => r(new Response(null, { status: 204 }))))
    const w = await mountDialog()
    await w.get('dialog textarea').setValue('hello')
    await form(w).trigger('submit')
    expect(submitBtn(w).attributes('disabled')).toBeDefined()
    expect(submitBtn(w).attributes('aria-busy')).toBe('true')
    await form(w).trigger('submit')
    finish()
    await flushPromises()
    expect(requests.filter((r) => r.url.endsWith('/feedback'))).toHaveLength(1)
  })

  it.each([
    [429, { error: 'too many feedback reports, please try again later' }, 'Too many feedback reports'],
    [502, { error: 'failed to send feedback' }, 'could not be sent'],
    [503, { error: 'feedback is not available on this instance' }, 'not available'],
    [400, { error: 'invalid feedback' }, 'between 1 and 2000'],
  ])('shows a translated error for %i', async (status, body, text) => {
    feedbackReply = () => json(body, status)
    const w = await mountDialog()
    await w.get('dialog textarea').setValue('hello')
    await form(w).trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain(text)
    expect(w.find('[data-testid="feedback-success"]').exists()).toBe(false)
    expect(submitBtn(w).attributes('disabled')).toBeUndefined()
  })

  it('shows French errors in the French UI', async () => {
    setLocale('fr')
    feedbackReply = () => json({ error: 'failed to send feedback' }, 502)
    const w = await mountDialog()
    await w.get('dialog textarea').setValue('salut')
    await form(w).trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain("n'a pas pu être envoyé")
  })
})
