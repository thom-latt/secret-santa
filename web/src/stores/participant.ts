import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface WishlistItem {
  id: string
  title: string
  url: string
  note: string
  sort_order: number
}

export interface ParticipantSpaceData {
  first_name: string
  last_name: string
  email: string
  event_title: string
  event_date: string
  budget: string
  description: string
  locale: 'fr' | 'en'
  revealed: boolean
  link_clicked_at: string | null
  target?: {
    first_name: string
    last_name: string
    wishlist: WishlistItem[]
  }
  own_wishlist: WishlistItem[]
}

export const useParticipantStore = defineStore('participant', () => {
  const data = ref<ParticipantSpaceData | null>(null)
  const notFound = ref(false)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchSpace(token: string) {
    loading.value = true
    error.value = null
    notFound.value = false
    try {
      const res = await fetch(`/api/p/${encodeURIComponent(token)}`)
      if (res.status >= 400 && res.status < 500) {
        notFound.value = true
        data.value = null
        return
      }
      const json = await res.json().catch(() => ({}))
      if (!res.ok) {
        throw new Error(json.error || 'Failed to load participant page')
      }
      data.value = json
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function revealTarget(token: string) {
    const res = await fetch(`/api/p/${encodeURIComponent(token)}/reveal`, {
      method: 'POST',
    })
    const json = await res.json()
    if (!res.ok) {
      throw new Error(json.error || 'Failed to reveal target')
    }
    data.value = json
  }

  async function addWishlistItem(token: string, payload: { title: string; url: string; note: string }) {
    const res = await fetch(`/api/p/${encodeURIComponent(token)}/wishlist`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const json = await res.json()
    if (!res.ok) {
      throw new Error(json.error || 'Failed to add wishlist item')
    }
    data.value = json
  }

  async function updateWishlistItem(
    token: string,
    itemId: string,
    payload: { title: string; url: string; note: string }
  ) {
    const res = await fetch(
      `/api/p/${encodeURIComponent(token)}/wishlist/${encodeURIComponent(itemId)}`,
      {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }
    )
    const json = await res.json()
    if (!res.ok) {
      throw new Error(json.error || 'Failed to update wishlist item')
    }
    data.value = json
  }

  async function deleteWishlistItem(token: string, itemId: string) {
    const res = await fetch(
      `/api/p/${encodeURIComponent(token)}/wishlist/${encodeURIComponent(itemId)}`,
      { method: 'DELETE' }
    )
    const json = await res.json()
    if (!res.ok) {
      throw new Error(json.error || 'Failed to delete wishlist item')
    }
    data.value = json
  }

  return {
    data,
    notFound,
    loading,
    error,
    fetchSpace,
    revealTarget,
    addWishlistItem,
    updateWishlistItem,
    deleteWishlistItem,
  }
})
