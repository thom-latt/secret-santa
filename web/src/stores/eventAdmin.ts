import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface AdminParticipant {
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

export interface Exclusion {
  id: string
  participant_a_id: string
  participant_b_id: string
}

export interface AdminEventData {
  id: string
  title: string
  description: string
  event_date: string
  budget: string
  organizer_first_name: string
  organizer_last_name: string
  organizer_email: string
  organizer_participates: boolean
  organizer_participant_token?: string
  status: 'draft' | 'drawn'
  locale: 'fr' | 'en'
  created_at: string
  drawn_at: string | null
  participants: AdminParticipant[]
  exclusions: Exclusion[]
  feasibility: {
    mode: 'single_cycle' | 'sub_loops' | 'impossible' | 'too_few'
    diagnostic: string
  }
}

export const useEventAdminStore = defineStore('eventAdmin', () => {
  const event = ref<AdminEventData | null>(null)
  const notFound = ref(false)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchEvent(adminToken: string) {
    loading.value = true
    error.value = null
    notFound.value = false
    try {
      const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}`)
      if (res.status >= 400 && res.status < 500) {
        notFound.value = true
        event.value = null
        return
      }
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        throw new Error(data.error || 'Failed to load event')
      }
      event.value = data
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function updateDetails(
    adminToken: string,
    payload: {
      title: string
      description: string
      event_date: string
      budget: string
      organizer_participates?: boolean
    }
  ) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to update event')
    }
    event.value = data
  }

  async function addParticipant(
    adminToken: string,
    payload: { first_name: string; last_name: string; email: string }
  ) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}/participants`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to add participant')
    }
    event.value = data
  }

  async function updateParticipant(
    adminToken: string,
    participantId: string,
    payload: { first_name: string; last_name: string; email: string }
  ) {
    const res = await fetch(
      `/api/admin/events/${encodeURIComponent(adminToken)}/participants/${encodeURIComponent(participantId)}`,
      {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }
    )
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to update participant')
    }
    event.value = data
  }

  async function deleteParticipant(adminToken: string, participantId: string) {
    const res = await fetch(
      `/api/admin/events/${encodeURIComponent(adminToken)}/participants/${encodeURIComponent(participantId)}`,
      { method: 'DELETE' }
    )
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to delete participant')
    }
    event.value = data
  }

  async function addExclusion(adminToken: string, participantAId: string, participantBId: string) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}/exclusions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ participant_a_id: participantAId, participant_b_id: participantBId }),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to add exclusion')
    }
    event.value = data
  }

  async function deleteExclusion(adminToken: string, exclusionId: string) {
    const res = await fetch(
      `/api/admin/events/${encodeURIComponent(adminToken)}/exclusions/${encodeURIComponent(exclusionId)}`,
      { method: 'DELETE' }
    )
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to delete exclusion')
    }
    event.value = data
  }

  async function executeDraw(adminToken: string) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}/draw`, {
      method: 'POST',
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to execute draw')
    }
    event.value = data
  }

  async function resendParticipantEmail(adminToken: string, participantId: string) {
    const res = await fetch(
      `/api/admin/events/${encodeURIComponent(adminToken)}/participants/${encodeURIComponent(participantId)}/resend`,
      { method: 'POST' }
    )
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to resend email')
    }
    event.value = data
  }

  async function resendFailedEmails(adminToken: string) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}/resend-failed`, {
      method: 'POST',
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to resend failed emails')
    }
    event.value = data
  }

  async function deleteEvent(adminToken: string) {
    const res = await fetch(`/api/admin/events/${encodeURIComponent(adminToken)}`, {
      method: 'DELETE',
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to delete event')
    }
    event.value = null
  }

  return {
    event,
    notFound,
    loading,
    error,
    fetchEvent,
    updateDetails,
    addParticipant,
    updateParticipant,
    deleteParticipant,
    addExclusion,
    deleteExclusion,
    executeDraw,
    resendParticipantEmail,
    resendFailedEmails,
    deleteEvent,
  }
})
