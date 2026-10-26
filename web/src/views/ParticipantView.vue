<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useParticipantStore, type WishlistItem } from '../stores/participant'
import ErrorView from './ErrorView.vue'

const { t } = useI18n()
const route = useRoute()
const store = useParticipantStore()

const token = computed(() => String(route.params.token || ''))

// Local screen toggle after target has been revealed on backend
const locallyVisible = ref(true)
const actionError = ref<string | null>(null)

// Own wishlist form
const newTitle = ref('')
const newUrl = ref('')
const newNote = ref('')

// Edit wishlist item
const editingItemId = ref<string | null>(null)
const editItemTitle = ref('')
const editItemUrl = ref('')
const editItemNote = ref('')

onMounted(async () => {
  await store.fetchSpace(token.value)
})

async function handleDiscover() {
  actionError.value = null
  try {
    await store.revealTarget(token.value)
    locallyVisible.value = true
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleAddWishlist() {
  actionError.value = null
  try {
    await store.addWishlistItem(token.value, {
      title: newTitle.value,
      url: newUrl.value,
      note: newNote.value,
    })
    newTitle.value = ''
    newUrl.value = ''
    newNote.value = ''
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

function startEditItem(item: WishlistItem) {
  editingItemId.value = item.id
  editItemTitle.value = item.title
  editItemUrl.value = item.url
  editItemNote.value = item.note
}

async function saveEditItem(itemId: string) {
  actionError.value = null
  try {
    await store.updateWishlistItem(token.value, itemId, {
      title: editItemTitle.value,
      url: editItemUrl.value,
      note: editItemNote.value,
    })
    editingItemId.value = null
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleDeleteItem(itemId: string) {
  actionError.value = null
  try {
    await store.deleteWishlistItem(token.value, itemId)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 py-8 space-y-6">
    <ErrorView v-if="store.notFound" :status="404" />
    <ErrorView
      v-else-if="store.error && !store.data"
      :status="500"
      :on-retry="() => store.fetchSpace(token)"
    />

    <template v-else-if="store.data">
      <!-- Event Header -->
      <header class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
        <p class="text-sm font-medium text-red-600 mb-1">
          {{ t('participant.welcome', { name: `${store.data.first_name} ${store.data.last_name}` }) }}
        </p>
        <h1 class="text-2xl font-bold text-slate-900">{{ store.data.event_title }}</h1>
        <div class="mt-3 flex flex-wrap gap-4 text-sm text-slate-600">
          <span v-if="store.data.event_date">📅 {{ store.data.event_date }}</span>
          <span v-if="store.data.budget">🎁 {{ store.data.budget }}</span>
        </div>
        <p v-if="store.data.description" class="mt-3 text-sm text-slate-700 whitespace-pre-line">
          {{ store.data.description }}
        </p>
      </header>

      <p v-if="actionError" class="rounded-xl bg-red-50 border border-red-200 p-4 text-sm text-red-700 font-medium">
        {{ actionError }}
      </p>

      <!-- Section 1: My Secret Santa Mission (US-5.2 & US-5.3) -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border-2 border-red-200 space-y-4">
        <div class="flex items-center justify-between">
          <h2 class="text-xl font-bold text-slate-900">{{ t('participant.mission_title') }}</h2>
          <button
            v-if="store.data.revealed"
            type="button"
            data-testid="toggle-visibility-btn"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            @click="locallyVisible = !locallyVisible"
          >
            {{ locallyVisible ? t('participant.toggle_hide') : t('participant.toggle_show') }}
          </button>
        </div>

        <!-- Strictly v-if (never rendered in DOM until revealed AND locallyVisible) -->
        <div v-if="!store.data.revealed" class="py-4 text-center space-y-4">
          <p class="text-sm text-slate-600">{{ t('participant.discover_prompt') }}</p>
          <button
            type="button"
            data-testid="discover-btn"
            class="min-h-[44px] rounded-xl bg-red-600 px-6 py-3 text-base font-bold text-white hover:bg-red-700 cursor-pointer"
            @click="handleDiscover"
          >
            {{ t('participant.discover_btn') }}
          </button>
        </div>

        <div v-else-if="locallyVisible && store.data.target" data-testid="revealed-target-section" class="space-y-4">
          <div class="rounded-xl bg-red-50 p-4 text-center border border-red-100">
            <p class="text-xs uppercase tracking-wider text-red-700 font-semibold mb-1">
              {{ t('participant.you_drew') }}
            </p>
            <p data-testid="target-full-name" class="text-2xl font-extrabold text-slate-900">
              {{ store.data.target.first_name }} {{ store.data.target.last_name }}
            </p>
          </div>

          <div>
            <h3 class="text-base font-bold text-slate-900 mb-2">
              {{ t('participant.target_wishlist_title', { name: store.data.target.first_name }) }}
            </h3>
            <p v-if="store.data.target.wishlist.length === 0" data-testid="target-wishlist-empty" class="text-sm text-slate-500 italic">
              {{ t('participant.target_wishlist_empty') }}
            </p>
            <ul v-else data-testid="target-wishlist" class="space-y-2">
              <li
                v-for="item in store.data.target.wishlist"
                :key="item.id"
                class="rounded-xl border border-slate-200 p-3 text-sm"
              >
                <div class="font-semibold text-slate-900">{{ item.title }}</div>
                <p v-if="item.note" class="text-slate-600 mt-1">{{ item.note }}</p>
                <a
                  v-if="item.url"
                  :href="item.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="mt-1 inline-block text-red-600 hover:underline break-all"
                >
                  {{ item.url }}
                </a>
              </li>
            </ul>
          </div>
        </div>
      </section>

      <!-- Section 2: My Wishlist (US-5.3 & US-5.4) -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-4">
        <div>
          <h2 class="text-xl font-bold text-slate-900">{{ t('participant.own_wishlist_title') }}</h2>
          <p data-testid="own-wishlist-reassurance" class="text-xs text-slate-500 mt-1">{{ t('participant.own_wishlist_reassurance') }}</p>
        </div>

        <form data-testid="add-wishlist-form" class="space-y-3" @submit.prevent="handleAddWishlist">
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label for="new-wishlist-title" class="block text-xs font-medium text-slate-700 mb-1">
                {{ t('participant.item_title') }}
              </label>
              <input
                id="new-wishlist-title"
                v-model="newTitle"
                data-testid="new-wishlist-title"
                type="text"
                required
                maxlength="200"
                :placeholder="t('participant.item_title')"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label for="new-wishlist-url" class="block text-xs font-medium text-slate-700 mb-1">
                {{ t('participant.item_url') }}
              </label>
              <input
                id="new-wishlist-url"
                v-model="newUrl"
                data-testid="new-wishlist-url"
                type="text"
                :placeholder="t('participant.item_url')"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
          </div>
          <div class="flex flex-col sm:flex-row sm:items-end gap-3">
            <div class="flex-1">
              <label for="new-wishlist-note" class="block text-xs font-medium text-slate-700 mb-1">
                {{ t('participant.item_note') }}
              </label>
              <input
                id="new-wishlist-note"
                v-model="newNote"
                data-testid="new-wishlist-note"
                type="text"
                maxlength="1000"
                :placeholder="t('participant.item_note')"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <button
              type="submit"
              data-testid="add-wishlist-btn"
              class="min-h-[44px] rounded-lg bg-slate-900 px-5 py-2 text-sm font-semibold text-white hover:bg-slate-800 cursor-pointer"
            >
              {{ t('participant.add_item_btn') }}
            </button>
          </div>
        </form>

        <p v-if="store.data.own_wishlist.length === 0" data-testid="own-wishlist-empty" class="text-sm text-slate-500 italic">
          {{ t('participant.own_wishlist_empty') }}
        </p>

        <div v-else data-testid="own-wishlist" class="space-y-3">
          <div
            v-for="item in store.data.own_wishlist"
            :key="item.id"
            class="rounded-xl border border-slate-200 p-4"
          >
            <div v-if="editingItemId === item.id" class="space-y-2">
              <input
                v-model="editItemTitle"
                data-testid="edit-wishlist-title"
                type="text"
                required
                maxlength="200"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <input
                v-model="editItemUrl"
                data-testid="edit-wishlist-url"
                type="text"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <input
                v-model="editItemNote"
                data-testid="edit-wishlist-note"
                type="text"
                maxlength="1000"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <div class="flex gap-2">
                <button
                  type="button"
                  data-testid="save-wishlist-btn"
                  class="min-h-[44px] rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white cursor-pointer"
                  @click="saveEditItem(item.id)"
                >
                  {{ t('admin.save_btn') }}
                </button>
                <button
                  type="button"
                  data-testid="cancel-wishlist-btn"
                  class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-xs font-medium text-slate-700 cursor-pointer"
                  @click="editingItemId = null"
                >
                  {{ t('admin.cancel_btn') }}
                </button>
              </div>
            </div>

            <div v-else class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div class="text-sm">
                <div class="font-semibold text-slate-900">{{ item.title }}</div>
                <p v-if="item.note" class="text-slate-600 mt-0.5">{{ item.note }}</p>
                <a
                  v-if="item.url"
                  :href="item.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-red-600 hover:underline break-all"
                >
                  {{ item.url }}
                </a>
              </div>
              <div class="flex gap-2">
                <button
                  type="button"
                  data-testid="edit-wishlist-btn"
                  class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 cursor-pointer"
                  @click="startEditItem(item)"
                >
                  {{ t('admin.edit_btn') }}
                </button>
                <button
                  type="button"
                  data-testid="delete-wishlist-btn"
                  class="min-h-[44px] rounded-lg border border-red-200 px-3 py-1.5 text-xs font-medium text-red-700 cursor-pointer"
                  @click="handleDeleteItem(item.id)"
                >
                  {{ t('admin.delete_btn') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>
