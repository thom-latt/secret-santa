import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAppConfigStore = defineStore('appConfig', () => {
  const allowPublicEventCreation = ref<boolean>(true)
  const loaded = ref<boolean>(false)

  async function fetchPublicConfig() {
    const res = await fetch('/api/public/config')
    if (!res.ok) {
      throw new Error('Failed to load public config')
    }
    const data = await res.json()
    allowPublicEventCreation.value = Boolean(data.allow_public_event_creation)
    loaded.value = true
  }

  async function createEvent(payload: {
    title: string
    first_name: string
    last_name: string
    email: string
    locale: string
  }): Promise<{ id: string; title: string; admin_token: string }> {
    const res = await fetch('/api/events', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to create event')
    }
    return data
  }

  async function recoverLinks(email: string, locale: string): Promise<string> {
    const res = await fetch('/api/recover', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, locale }),
    })
    const data = await res.json()
    if (!res.ok) {
      throw new Error(data.error || 'Failed to recover links')
    }
    return data.message
  }

  return {
    allowPublicEventCreation,
    loaded,
    fetchPublicConfig,
    createEvent,
    recoverLinks,
  }
})
