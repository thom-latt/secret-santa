import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n, setLocale } from '../i18n'
import LangSwitcher from '../components/LangSwitcher.vue'
import HomeView from '../views/HomeView.vue'
import ParticipantView from '../views/ParticipantView.vue'
import { useAppConfigStore } from '../stores/appConfig'
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
})
