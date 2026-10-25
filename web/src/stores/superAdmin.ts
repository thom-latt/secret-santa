import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface SuperAdminEventSummary {
  id: string
  title: string
  organizer_first_name: string
  organizer_last_name: string
  organizer_email: string
  status: 'draft' | 'drawn'
  locale: 'fr' | 'en'
  created_at: string
  drawn_at: string | null
  participant_count: number
  revealed_targets_count: number
  filled_wishlists_count: number
}

export const useSuperAdminStore = defineStore('superAdmin', () => {
  const authenticated = ref(false)
  const allowPublicEventCreation = ref(true)
  const superAdminEmail = ref('')
  const events = ref<SuperAdminEventSummary[]>([])
  const loading = ref(false)

  async function fetchOverview() {
    loading.value = true
    try {
      const res = await fetch('/api/superadmin/overview')
      if (res.status === 401) {
        authenticated.value = false
        return
      }
      const data = await res.json()
      if (!res.ok) {
        throw new Error(data.error || 'Failed to load overview')
      }
      authenticated.value = true
      allowPublicEventCreation.value = Boolean(data.allow_public_event_creation)
      superAdminEmail.value = data.super_admin_email
      events.value = data.events || []
    } finally {
      loading.value = false
    }
  }

  async function login(email: string, password: string) {
    const res = await fetch('/api/superadmin/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Invalid credentials')
    }
    await fetchOverview()
  }

  async function logout() {
    await fetch('/api/superadmin/logout', { method: 'POST' })
    authenticated.value = false
    events.value = []
  }

  async function togglePublicCreation(allow: boolean) {
    const res = await fetch('/api/superadmin/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ allow_public_event_creation: allow }),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to update setting')
    }
    allowPublicEventCreation.value = Boolean(data.allow_public_event_creation)
  }

  async function inviteOrganizer(payload: {
    title: string
    first_name: string
    last_name: string
    email: string
    locale: string
  }) {
    const res = await fetch('/api/superadmin/invite', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to invite organizer')
    }
    await fetchOverview()
  }

  async function testSmtp() {
    const res = await fetch('/api/superadmin/test-smtp', { method: 'POST' })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'SMTP test failed')
    }
    return data.message
  }

  async function resendAdminLink(eventId: string) {
    const res = await fetch(`/api/superadmin/events/${encodeURIComponent(eventId)}/resend-admin`, {
      method: 'POST',
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to resend link')
    }
  }

  async function deleteEvent(eventId: string) {
    const res = await fetch(`/api/superadmin/events/${encodeURIComponent(eventId)}`, {
      method: 'DELETE',
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to delete event')
    }
    await fetchOverview()
  }

  return {
    authenticated,
    allowPublicEventCreation,
    superAdminEmail,
    events,
    loading,
    fetchOverview,
    login,
    logout,
    togglePublicCreation,
    inviteOrganizer,
    testSmtp,
    resendAdminLink,
    deleteEvent,
  }
})
