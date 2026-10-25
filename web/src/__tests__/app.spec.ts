import { describe, it, expect, beforeEach } from 'vitest'
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
    await wrapper.get('[data-testid="lang-en"]').trigger('click')
    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('secret_santa_locale')).toBe('en')
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
