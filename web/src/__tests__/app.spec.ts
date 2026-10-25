import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n, setLocale } from '../i18n'
import LangSwitcher from '../components/LangSwitcher.vue'
import HomeView from '../views/HomeView.vue'
import EventAdminView from '../views/EventAdminView.vue'
import ParticipantView from '../views/ParticipantView.vue'
import SuperAdminView from '../views/SuperAdminView.vue'
import { useAppConfigStore } from '../stores/appConfig'
import { useEventAdminStore } from '../stores/eventAdmin'
import { useParticipantStore } from '../stores/participant'
import { useSuperAdminStore } from '../stores/superAdmin'
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

  it('handles draw disabled message (<3 participants), confirmation modal, prior detail save, and post-draw locks (Epic 3)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/admin/events/adm_tok_draw')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const adminStore = useEventAdminStore(pinia)

    adminStore.notFound = false
    adminStore.event = {
      id: 'ev_draw',
      title: 'Noël Bureau',
      description: '',
      event_date: '',
      budget: '',
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
      ],
      exclusions: [],
      feasibility: {
        mode: 'too_few',
        diagnostic: 'Need 1 more participant(s) (minimum 3)',
      },
    }

    const wrapper = mount(EventAdminView, {
      global: { plugins: [pinia, router, i18n] },
    })

    // With 2 participants, button is disabled and explicit message shows how many more are needed (1)
    expect(wrapper.get('[data-testid="launch-draw-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="draw-need-more-msg"]').text()).toContain('1')

    // Add 3rd participant and make feasibility valid
    adminStore.event.participants.push({
      id: 'p3',
      first_name: 'Charlie',
      last_name: 'Durand',
      email: 'charlie@example.com',
      is_organizer: false,
      link_clicked_at: null,
      email_sent_at: null,
      email_error: null,
      wishlist_count: 0,
    })
    adminStore.event.feasibility = { mode: 'single_cycle', diagnostic: '' }
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="draw-need-more-msg"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="launch-draw-btn"]').attributes('disabled')).toBeUndefined()

    // Edit budget in form without clicking "Save changes"
    const budgetInput = wrapper.find('input[placeholder="Ex : 20 €"]')
    await budgetInput.setValue('35 €')

    // Click Launch Draw -> opens confirmation modal, then confirm -> saves details first then executes draw
    await wrapper.get('[data-testid="launch-draw-btn"]').trigger('click')
    await wrapper.get('[data-testid="confirm-draw-btn"]').trigger('click')

    expect(adminStore.updateDetails).toHaveBeenCalledWith('adm_tok_draw', {
      title: 'Noël Bureau',
      description: '',
      event_date: '',
      budget: '35 €',
      organizer_participates: true,
    })
    expect(adminStore.executeDraw).toHaveBeenCalledWith('adm_tok_draw')

    // Simulate transition to 'drawn' status: add/delete participant and exclusions sections are locked
    adminStore.event.status = 'drawn'
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="add-participant-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="add-exclusion-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="launch-draw-btn"]').exists()).toBe(false)
  })

  it('tracks post-draw status without draw disclosure, links to organizer page, edits metadata/participants, resends emails, and deletes event (Epic 4)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/admin/events/adm_tok_post')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const adminStore = useEventAdminStore(pinia)

    adminStore.notFound = false
    adminStore.event = {
      id: 'ev_post',
      title: 'Noël Famille',
      description: 'RDV 19h',
      event_date: '2026-12-24',
      budget: '30 €',
      organizer_first_name: 'Alice',
      organizer_last_name: 'Martin',
      organizer_email: 'alice@example.com',
      organizer_participates: true,
      organizer_participant_token: 'part_tok_org',
      status: 'drawn',
      locale: 'fr',
      created_at: '2026-10-25T12:00:00Z',
      drawn_at: '2026-10-25T13:00:00Z',
      participants: [
        {
          id: 'p1',
          first_name: 'Alice',
          last_name: 'Martin',
          email: 'alice@example.com',
          is_organizer: true,
          link_clicked_at: '2026-10-25T15:30:00Z',
          email_sent_at: '2026-10-25T13:00:01Z',
          email_error: null,
          wishlist_count: 2,
        },
        {
          id: 'p2',
          first_name: 'Bob',
          last_name: 'Dupont',
          email: 'bob@example.com',
          is_organizer: false,
          link_clicked_at: null,
          email_sent_at: null,
          email_error: 'smtp timeout',
          wishlist_count: 0,
        },
      ],
      exclusions: [],
      feasibility: {
        mode: 'single_cycle',
        diagnostic: '',
      },
    }

    const wrapper = mount(EventAdminView, {
      global: { plugins: [pinia, router, i18n] },
    })

    // US-4.1: 3 indicators with icon (aria-hidden="true") + accessible text for each participant
    const emailBadges = wrapper.findAll('[data-testid="indicator-email"]')
    const revealedBadges = wrapper.findAll('[data-testid="indicator-revealed"]')
    const wishlistBadges = wrapper.findAll('[data-testid="indicator-wishlist"]')

    expect(emailBadges[0].find('[aria-hidden="true"]').exists()).toBe(true)
    expect(emailBadges[0].text()).toContain('Statut Email: Envoyé')
    expect(revealedBadges[0].text()).toContain('Cible découverte: Oui (2026-10-25)')
    expect(wishlistBadges[0].text()).toContain('Liste remplie: Oui (2)')

    expect(emailBadges[1].text()).toContain('Statut Email: Échec')
    expect(revealedBadges[1].text()).toContain('Cible découverte: En attente')
    expect(wishlistBadges[1].text()).toContain('Liste remplie: Vide')

    // Global "Resend failed emails" button displayed when at least one participant has email_sent_at === null
    const resendFailedBtn = wrapper.get('[data-testid="resend-failed-btn"]')
    await resendFailedBtn.trigger('click')
    expect(adminStore.resendFailedEmails).toHaveBeenCalledWith('adm_tok_post')

    adminStore.event.participants[1].email_sent_at = '2026-10-25T16:00:00Z'
    adminStore.event.participants[1].email_error = null
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="resend-failed-btn"]').exists()).toBe(false)

    // US-4.2: "Go to my participant page" button displayed only if organizer_participates == true and status == 'drawn'
    const orgLink = wrapper.get('[data-testid="organizer-participant-link"]')
    expect(orgLink.attributes('href')).toBe('/p/part_tok_org')

    adminStore.event.organizer_participates = false
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="organizer-participant-link"]').exists()).toBe(false)
    adminStore.event.organizer_participates = true
    await wrapper.vm.$nextTick()

    // US-4.3: Edit participant identity/email in drawn status (no delete button shown) and resend individual email
    expect(wrapper.find('[data-testid="delete-participant-btn"]').exists()).toBe(false)
    const editBtns = wrapper.findAll('[data-testid="edit-participant-btn"]')
    await editBtns[1].trigger('click')

    await wrapper.get('[data-testid="edit-participant-first-name"]').setValue('Bobby')
    await wrapper.get('[data-testid="edit-participant-last-name"]').setValue('Dupont-Morel')
    await wrapper.get('[data-testid="edit-participant-email"]').setValue('bobby@example.com')
    await wrapper.get('[data-testid="save-participant-btn"]').trigger('click')

    expect(adminStore.updateParticipant).toHaveBeenCalledWith('adm_tok_post', 'p2', {
      first_name: 'Bobby',
      last_name: 'Dupont-Morel',
      email: 'bobby@example.com',
    })

    const resendBtns = wrapper.findAll('[data-testid="resend-participant-btn"]')
    await resendBtns[1].trigger('click')
    expect(adminStore.resendParticipantEmail).toHaveBeenCalledWith('adm_tok_post', 'p2')

    // US-4.4: Delete event via confirmation modal and redirect to '/'
    await wrapper.get('[data-testid="delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="confirm-delete-btn"]').trigger('click')
    expect(adminStore.deleteEvent).toHaveBeenCalledWith('adm_tok_post')
  })

  it('handles ParticipantView 404, interactive reveal, local hide/show toggle, target wishlist links, and own wishlist CRUD (Epic 5)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/p/tok_epic5')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const partStore = useParticipantStore(pinia)

    // 1. 404 when participant token is invalid or event is still draft
    partStore.notFound = true
    const wrapper404 = mount(ParticipantView, {
      global: { plugins: [pinia, router, i18n] },
    })
    expect(wrapper404.find('[data-testid="not-found"]').exists()).toBe(true)

    // 2. Initial unrevealed state (US-5.1 & US-5.2)
    partStore.notFound = false
    partStore.data = {
      first_name: 'Alice',
      last_name: 'Martin',
      email: 'alice@example.com',
      event_title: 'Noël Epic 5',
      event_date: '2026-12-25',
      budget: '25 €',
      description: 'Apporter des chocolats',
      locale: 'fr',
      revealed: false,
      link_clicked_at: null,
      own_wishlist: [],
    }

    const wrapper = mount(ParticipantView, {
      global: { plugins: [pinia, router, i18n] },
    })

    // Event details displayed, target hidden
    expect(wrapper.text()).toContain('Bonjour Alice Martin')
    expect(wrapper.text()).toContain('Noël Epic 5')
    expect(wrapper.text()).toContain('2026-12-25')
    expect(wrapper.text()).toContain('25 €')
    expect(wrapper.text()).toContain('Apporter des chocolats')
    expect(wrapper.find('[data-testid="discover-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="toggle-visibility-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(false)

    // Click Discover -> calls revealTarget(token)
    vi.mocked(partStore.revealTarget).mockImplementation(async () => {
      partStore.data = {
        ...partStore.data!,
        revealed: true,
        link_clicked_at: '2026-10-25T18:00:00Z',
        target: {
          first_name: 'Bob',
          last_name: 'Dupont',
          wishlist: [],
        },
      }
    })
    await wrapper.get('[data-testid="discover-btn"]').trigger('click')
    expect(partStore.revealTarget).toHaveBeenCalledWith('tok_epic5')

    // Target revealed with empty wishlist state (US-5.3)
    expect(wrapper.find('[data-testid="discover-btn"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="target-full-name"]').text()).toBe('Bob Dupont')
    expect(wrapper.find('[data-testid="target-wishlist-empty"]').exists()).toBe(true)

    // Populate target wishlist and verify external link attributes (target="_blank" rel="noopener noreferrer")
    partStore.data.target!.wishlist = [
      {
        id: 'tw1',
        title: 'Casque Audio',
        url: 'https://example.com/headphones',
        note: 'Noir mat',
        sort_order: 1,
      },
    ]
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="target-wishlist-empty"]').exists()).toBe(false)
    const targetLink = wrapper.get('[data-testid="target-wishlist"] a')
    expect(targetLink.attributes('href')).toBe('https://example.com/headphones')
    expect(targetLink.attributes('target')).toBe('_blank')
    expect(targetLink.attributes('rel')).toBe('noopener noreferrer')

    // Local hide/show toggle removes and restores target from DOM via v-if (US-5.2)
    const toggleBtn = wrapper.get('[data-testid="toggle-visibility-btn"]')
    expect(toggleBtn.text()).toContain('Masquer le nom')
    await toggleBtn.trigger('click')
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="target-full-name"]').exists()).toBe(false)
    expect(toggleBtn.text()).toContain('Afficher le nom')
    await toggleBtn.trigger('click')
    expect(wrapper.find('[data-testid="revealed-target-section"]').exists()).toBe(true)

    // 3. Own Wishlist CRUD & Reassurance Note (US-5.4)
    expect(wrapper.get('[data-testid="own-wishlist-reassurance"]').text()).toContain(
      'Visible uniquement par votre Père Noël secret. Aucun email n\'est envoyé lorsque vous modifiez votre liste.',
    )
    expect(wrapper.find('[data-testid="own-wishlist-empty"]').exists()).toBe(true)

    // Add item
    await wrapper.get('[data-testid="new-wishlist-title"]').setValue('Livre Go')
    await wrapper.get('[data-testid="new-wishlist-url"]').setValue('amazon.fr/dp/123')
    await wrapper.get('[data-testid="new-wishlist-note"]').setValue('Broché')
    await wrapper.get('[data-testid="add-wishlist-form"]').trigger('submit.prevent')

    expect(partStore.addWishlistItem).toHaveBeenCalledWith('tok_epic5', {
      title: 'Livre Go',
      url: 'amazon.fr/dp/123',
      note: 'Broché',
    })

    // Simulate store update with new own wishlist item
    partStore.data.own_wishlist = [
      {
        id: 'ow1',
        title: 'Livre Go',
        url: 'https://amazon.fr/dp/123',
        note: 'Broché',
        sort_order: 1,
      },
    ]
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="own-wishlist-empty"]').exists()).toBe(false)
    const ownLink = wrapper.get('[data-testid="own-wishlist"] a')
    expect(ownLink.attributes('href')).toBe('https://amazon.fr/dp/123')
    expect(ownLink.attributes('target')).toBe('_blank')
    expect(ownLink.attributes('rel')).toBe('noopener noreferrer')

    // Edit own wishlist item
    await wrapper.get('[data-testid="edit-wishlist-btn"]').trigger('click')
    await wrapper.get('[data-testid="edit-wishlist-title"]').setValue('Livre Rust')
    await wrapper.get('[data-testid="edit-wishlist-url"]').setValue('https://amazon.fr/dp/456')
    await wrapper.get('[data-testid="edit-wishlist-note"]').setValue('2e édition')
    await wrapper.get('[data-testid="save-wishlist-btn"]').trigger('click')

    expect(partStore.updateWishlistItem).toHaveBeenCalledWith('tok_epic5', 'ow1', {
      title: 'Livre Rust',
      url: 'https://amazon.fr/dp/456',
      note: '2e édition',
    })

    // Delete own wishlist item
    await wrapper.get('[data-testid="delete-wishlist-btn"]').trigger('click')
    expect(partStore.deleteWishlistItem).toHaveBeenCalledWith('tok_epic5', 'ow1')
  })

  it('handles SuperAdminView login, public creation switch, organizer invitation, SMTP test, event list metrics, admin link resend, and event deletion (Epic 6)', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/superadmin')
    await router.isReady()

    const pinia = createTestingPinia({ stubActions: true })
    const saStore = useSuperAdminStore(pinia)
    saStore.authenticated = false

    const wrapper = mount(SuperAdminView, {
      global: { plugins: [pinia, router, i18n] },
    })

    // 1. US-6.1: Unauthenticated login form & error handling
    const loginForm = wrapper.get('[data-testid="superadmin-login-form"]')
    const loginInputs = loginForm.findAll('input')
    await loginInputs[0].setValue('super@example.com')
    await loginInputs[1].setValue('wrong')
    vi.mocked(saStore.login).mockRejectedValueOnce(new Error('invalid credentials'))
    await loginForm.trigger('submit.prevent')
    expect(wrapper.text()).toContain('invalid credentials')

    // Valid login transitions to authenticated back-office
    vi.mocked(saStore.login).mockImplementationOnce(async () => {
      saStore.authenticated = true
      saStore.superAdminEmail = 'super@example.com'
      saStore.allowPublicEventCreation = true
      saStore.events = [
        {
          id: 'ev_sa_1',
          title: 'Noël Famille 2026',
          organizer_first_name: 'Claire',
          organizer_last_name: 'Martin',
          organizer_email: 'claire@example.com',
          status: 'drawn',
          locale: 'fr',
          created_at: '2026-10-25T10:15:00Z',
          drawn_at: '2026-10-25T11:00:00Z',
          participant_count: 6,
          revealed_targets_count: 4,
          filled_wishlists_count: 5,
        },
      ]
    })
    await loginInputs[1].setValue('secret-password-123')
    await loginForm.trigger('submit.prevent')
    expect(saStore.login).toHaveBeenCalledWith('super@example.com', 'secret-password-123')

    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="superadmin-login-form"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('super@example.com')

    // 2. US-6.2: Toggle public event creation switch
    const toggleBtn = wrapper.get('[data-testid="toggle-public-creation"]')
    expect(toggleBtn.attributes('aria-checked')).toBe('true')
    expect(toggleBtn.text()).toBe('ON')

    vi.mocked(saStore.togglePublicCreation).mockImplementationOnce(async (allow: boolean) => {
      saStore.allowPublicEventCreation = allow
    })
    await toggleBtn.trigger('click')
    expect(saStore.togglePublicCreation).toHaveBeenCalledWith(false)
    await wrapper.vm.$nextTick()
    expect(toggleBtn.attributes('aria-checked')).toBe('false')
    expect(toggleBtn.text()).toBe('OFF')

    // 3. US-6.3: Invite an organizer
    await wrapper.get('[data-testid="invite-title"]').setValue('Secret Santa Amis')
    await wrapper.get('[data-testid="invite-first-name"]').setValue('Paul')
    await wrapper.get('[data-testid="invite-last-name"]').setValue('Bernard')
    await wrapper.get('[data-testid="invite-email"]').setValue('paul@example.com')
    await wrapper.get('[data-testid="invite-organizer-form"]').trigger('submit.prevent')

    expect(saStore.inviteOrganizer).toHaveBeenCalledWith({
      title: 'Secret Santa Amis',
      first_name: 'Paul',
      last_name: 'Bernard',
      email: 'paul@example.com',
      locale: 'fr',
    })
    expect(wrapper.get('[data-testid="superadmin-status-message"]').text()).toContain(
      'Invitation envoyée !',
    )

    // 4. US-6.4: Global events view metrics, SMTP test, resend admin link, and delete event with confirmation
    const card = wrapper.get('[data-testid="superadmin-event-card"]')
    expect(card.text()).toContain('Noël Famille 2026')
    expect(card.text()).toContain('Claire Martin (claire@example.com)')
    expect(card.get('[data-testid="event-created-at"]').text()).toContain('2026-10-25')
    expect(card.get('[data-testid="event-participants-count"]').text()).toContain('6')
    expect(card.get('[data-testid="event-revealed-count"]').text()).toContain('4/6')
    expect(card.get('[data-testid="event-wishlists-count"]').text()).toContain('5/6')

    // SMTP test
    vi.mocked(saStore.testSmtp).mockResolvedValueOnce('test email sent successfully')
    await wrapper.get('[data-testid="test-smtp-btn"]').trigger('click')
    expect(saStore.testSmtp).toHaveBeenCalled()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="superadmin-status-message"]').text()).toBe(
      'test email sent successfully',
    )

    // Resend admin link
    await wrapper.get('[data-testid="resend-admin-link-btn"]').trigger('click')
    expect(saStore.resendAdminLink).toHaveBeenCalledWith('ev_sa_1')
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="superadmin-status-message"]').text()).toContain(
      "Lien d'administration renvoyé !",
    )

    // Delete event via confirmation modal
    await wrapper.get('[data-testid="superadmin-delete-event-btn"]').trigger('click')
    await wrapper.get('[data-testid="superadmin-confirm-delete-btn"]').trigger('click')
    expect(saStore.deleteEvent).toHaveBeenCalledWith('ev_sa_1')

    // Logout
    await wrapper.get('[data-testid="superadmin-logout-btn"]').trigger('click')
    expect(saStore.logout).toHaveBeenCalled()
  })
})
