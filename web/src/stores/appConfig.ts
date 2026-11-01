import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiErrorMessage, readJson } from './http'

export const useAppConfigStore = defineStore('appConfig', () => {
  const allowPublicEventCreation = ref<boolean>(false)
  const operatorName = ref<string>('')
  const operatorEmail = ref<string>('')
  const loaded = ref<boolean>(false)

  async function fetchPublicConfig() {
    const res = await fetch('/api/public/config')
    if (!res.ok) {
      throw new Error(apiErrorMessage(res, {}))
    }
    const data = await readJson(res)
    allowPublicEventCreation.value = Boolean(data.allow_public_event_creation)
    operatorName.value = String(data.operator_name ?? '')
    operatorEmail.value = String(data.operator_email ?? '')
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
    const data = await readJson(res)
    if (!res.ok) {
      throw new Error(apiErrorMessage(res, data))
    }
    return data
  }

  async function recoverLinks(email: string, locale: string): Promise<string> {
    const res = await fetch('/api/recover', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, locale }),
    })
    const data = await readJson(res)
    if (!res.ok) {
      throw new Error(apiErrorMessage(res, data))
    }
    return data.message
  }

  return {
    allowPublicEventCreation,
    operatorName,
    operatorEmail,
    loaded,
    fetchPublicConfig,
    createEvent,
    recoverLinks,
  }
})
