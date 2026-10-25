import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n, setLocale } from '../i18n'
import LangSwitcher from '../components/LangSwitcher.vue'
import HomeView from '../views/HomeView.vue'
import EventAdminView from '../views/EventAdminView.vue'
import ParticipantView from '../views/ParticipantView.vue'
import { useAppConfigStore } from '../stores/appConfig'
import { useEventAdminStore } from '../stores/eventAdmin'
import { useParticipantStore } from '../stores/participant'
import { routes } from '../router'

describe('Secret Santa Frontend', () => {
  beforeEach(() => {
    setLocale('fr')
  })

  it('switches language between FR and EN and persists to localStorage', async () => {
    const wrapper = mount(LangSwitcher, {
      global: {
        plugins: [i18n],
      },
    })

    expect(i18n.global.locale.value).toBe('fr')
    expect(document.documentElement.lang).toBe('fr')
    await wrapper.get('[data-testid="lang-en"]').trigger('click')
    expect(i18n.global.locale.value).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    expect(localStorage.getItem('secret_santa_locale')).toBe('en')
  })

  it('detects initial locale from localStorage then navigator.language with fallback to fr', async () => {
    const { detectInitialLocale } = await import('../i18n')
    localStorage.removeItem('secret_santa_locale')
    const origLang = navigator.language
    Object.defineProperty(navigator, 'language', { value: 'en-US', configurable: true })
    expect(detectInitialLocale()).toBe('en')

    Object.defineProperty(navigator, 'language', { value: 'es-ES', configurable: true })
    expect(detectInitialLocale()).toBe('fr')

    localStorage.setItem('secret_santa_locale', 'en')
    expect(detectInitialLocale()).toBe('en')
    Object.defineProperty(navigator, 'language', { value: origLang, configurable: true })
  })

  it('creates an event with active locale and recovers links from HomeView', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const configStore = useAppConfigStore(pinia)
    configStore.allowPublicEventCreation = true
    vi.mocked(configStore.createEvent).mockResolvedValue({
      id: 'ev_1',
      title: 'Xmas 2026',
      admin_token: 'adm_tok_1',
    })
    vi.mocked(configStore.recoverLinks).mockResolvedValue(
      'If an event is associated with this address, an email has been sent.',
    )

    setLocale('en')
    const wrapper = mount(HomeView, {
      global: {
        plugins: [pinia, router, i18n],
      },
    })

    await wrapper.get('#event-title').setValue('Xmas 2026')
    await wrapper.get('#org-first-name').setValue('Alice')
    await wrapper.get('#org-last-name').setValue('Martin')
    await wrapper.get('#org-email').setValue('alice@example.com')
    await wrapper.get('[data-testid="create-event-form"]').trigger('submit.prevent')

    expect(configStore.createEvent).toHaveBeenCalledWith({
      title: 'Xmas 2026',
      first_name: 'Alice',
      last_name: 'Martin',
      email: 'alice@example.com',
      locale: 'en',
    })

    const recoverForm = wrapper.get('[data-testid="recover-form"]')
    await recoverForm.get('input[type="email"]').setValue('alice@example.com')
    await recoverForm.trigger('submit.prevent')

    expect(configStore.recoverLinks).toHaveBeenCalledWith('alice@example.com', 'en')
    expect(wrapper.get('[data-testid="recover-message"]').text()).toContain(
      'If an event is associated with this address',
    )
  })

  it('hides public event creation form when disabled by Super Admin', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const pinia = createTestingPinia({ stubActions: true })
    const configStore = useAppConfigStore(pinia)
    configStore.allowPublicEventCreation = false

    const wrapper = mount(HomeView, {
      global: {
        plugins: [pinia, router, i18n],
      },
    })

    expect(wrapper.find('[data-testid="public-disabled-banner"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="create-event-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="recover-form"]').exists()).toBe(true)
  })

  it('strictly omits target name and wishlist from DOM until Discover is clicked', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/p/tok_123')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const partStore = useParticipantStore(pinia)

    partStore.data = {
      first_name: 'Alice',
      last_name: 'Martin',
      email: 'alice@example.com',
      event_title: 'Noël 2026',
      event_date: '2026-12-25',
      budget: '20 €',
      description: '',
      locale: 'fr',
      revealed: false,
      link_clicked_at: null,
      own_wishlist: [],
    }

    const wrapper = mount(ParticipantView, {
      global: {
        plugins: [pinia, router, i18n],
      },
    })

    // Before reveal: Discover button is shown, target name and wishlist are NOT in DOM
    expect(wrapper.find('[data-testid="discover-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="target-full-name"]').exists()).toBe(false)

    // Simulate reveal response from POST /api/p/:token/reveal
    partStore.data = {
      ...partStore.data,
      revealed: true,
      link_clicked_at: '2026-10-25T15:00:00Z',
      target: {
        first_name: 'Bob',
        last_name: 'Dupont',
        wishlist: [
          {
            id: 'w1',
            title: 'Livre SF',
            url: 'https://example.com/book',
            note: 'Poche',
            sort_order: 1,
          },
        ],
      },
    }
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="discover-btn"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="target-full-name"]').text()).toBe('Bob Dupont')
    expect(wrapper.get('[data-testid="target-wishlist"]').text()).toContain('Livre SF')
  })

  it('renders 404 on invalid adminToken and manages draft event in EventAdminView (Epic 2)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/admin/events/adm_tok_123')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const adminStore = useEventAdminStore(pinia)

    // US-2.1: 404 view when token is invalid
    adminStore.notFound = true
    const wrapper404 = mount(EventAdminView, {
      global: { plugins: [pinia, router, i18n] },
    })
    expect(wrapper404.find('[data-testid="not-found"]').exists()).toBe(true)

    // US-2.1, US-2.2, US-2.3, US-2.4, US-2.5: Valid draft event
    adminStore.notFound = false
    adminStore.event = {
      id: 'ev_1',
      title: 'Noël 2026',
      description: 'Soirée',
      event_date: '2026-12-25',
      budget: '25 €',
      organizer_first_name: 'Alice',
      organizer_last_name: 'Martin',
      organizer_email: 'alice@example.com',
      organizer_participates: true,
      status: 'draft',
      locale: 'fr',
      created_at: '2026-10-25T12:00:00Z',
      drawn_at: null,
      participants: [
        {
          id: 'p1',
          first_name: 'Alice',
          last_name: 'Martin',
          email: 'alice@example.com',
          is_organizer: true,
          link_clicked_at: null,
          email_sent_at: null,
          email_error: null,
          wishlist_count: 0,
        },
        {
          id: 'p2',
          first_name: 'Bob',
          last_name: 'Dupont',
          email: 'bob@example.com',
          is_organizer: false,
          link_clicked_at: null,
          email_sent_at: null,
          email_error: null,
          wishlist_count: 0,
        },
        {
          id: 'p3',
          first_name: 'Charlie',
          last_name: 'Durand',
          email: 'charlie@example.com',
          is_organizer: false,
          link_clicked_at: null,
          email_sent_at: null,
          email_error: null,
          wishlist_count: 0,
        },
      ],
      exclusions: [{ id: 'ex1', participant_a_id: 'p1', participant_b_id: 'p2' }],
      feasibility: {
        mode: 'single_cycle',
        diagnostic: '',
      },
    }

    const wrapper = mount(EventAdminView, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    })

    // Organizer badge & feasibility indicator (single_cycle)
    expect(wrapper.text()).toContain('Organisateur')
    expect(wrapper.get('[data-testid="feasibility-badge"]').text()).toContain(
      'Tirage valide (Chaîne unique)',
    )

    // US-2.3: Add participant with autocomplete="off" and focus returned to First Name input
    const addForm = wrapper.get('[data-testid="add-participant-form"]')
    const inputs = addForm.findAll('input')
    expect(inputs[0].attributes('autocomplete')).toBe('off')
    expect(inputs[1].attributes('autocomplete')).toBe('off')
    expect(inputs[2].attributes('autocomplete')).toBe('off')

    await inputs[0].setValue('Diane')
    await inputs[1].setValue('Moreau')
    await inputs[2].setValue('diane@example.com')
    await addForm.trigger('submit.prevent')

    expect(adminStore.addParticipant).toHaveBeenCalledWith('adm_tok_123', {
      first_name: 'Diane',
      last_name: 'Moreau',
      email: 'diane@example.com',
    })
    expect(document.activeElement).toBe(inputs[0].element)

    // US-2.4: Exclusion selector prevents selecting same participant or already excluded pair (p1 <-> p2)
    const selectA = wrapper.get('[data-testid="exclusion-select-a"]')
    const selectB = wrapper.get('[data-testid="exclusion-select-b"]')
    await selectA.setValue('p1')
    const optionsB = selectB.findAll('option')
    // option 0 is placeholder, option 1 is p1 (self), option 2 is p2 (already excluded with p1), option 3 is p3 (allowed)
    expect(optionsB[1].attributes('disabled')).toBeDefined()
    expect(optionsB[2].attributes('disabled')).toBeDefined()
    expect(optionsB[3].attributes('disabled')).toBeUndefined()

    await selectB.setValue('p3')
    await wrapper.get('[data-testid="add-exclusion-form"]').trigger('submit.prevent')
    expect(adminStore.addExclusion).toHaveBeenCalledWith('adm_tok_123', 'p1', 'p3')

    // US-2.5: Verify sub_loops and impossible feasibility states
    adminStore.event.feasibility = { mode: 'sub_loops', diagnostic: '' }
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="feasibility-badge"]').text()).toContain(
      'Tirage valide (Sous-boucles de 3+ personnes)',
    )

    adminStore.event.feasibility = {
      mode: 'impossible',
      diagnostic: 'Participant Alice Martin has fewer than 2 allowed partners',
    }
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="feasibility-badge"]').text()).toContain('Tirage impossible')
    expect(wrapper.get('[data-testid="feasibility-badge"]').text()).toContain('Alice Martin')
    expect(wrapper.get('[data-testid="launch-draw-btn"]').attributes('disabled')).toBeDefined()

    wrapper.unmount()
  })
})
