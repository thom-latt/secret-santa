<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppConfigStore } from '../stores/appConfig'

const { t, locale } = useI18n()
const router = useRouter()
const configStore = useAppConfigStore()

const title = ref('')
const firstName = ref('')
const lastName = ref('')
const email = ref('')
const creating = ref(false)
const createError = ref<string | null>(null)

const recoverEmail = ref('')
const recoverMessage = ref<string | null>(null)
const recoverError = ref<string | null>(null)

onMounted(async () => {
  try {
    await configStore.fetchPublicConfig()
  } catch {
    // ignore initial load error in tests
  }
})

async function handleCreate() {
  createError.value = null
  creating.value = true
  try {
    const res = await configStore.createEvent({
      title: title.value,
      first_name: firstName.value,
      last_name: lastName.value,
      email: email.value,
      locale: locale.value,
    })
    await router.push(`/admin/events/${res.admin_token}`)
  } catch (e: unknown) {
    createError.value = e instanceof Error ? e.message : String(e)
  } finally {
    creating.value = false
  }
}

async function handleRecover() {
  recoverError.value = null
  recoverMessage.value = null
  try {
    const msg = await configStore.recoverLinks(recoverEmail.value, locale.value)
    recoverMessage.value = msg || t('home.recover_sent')
    recoverEmail.value = ''
  } catch (e: unknown) {
    recoverError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-8 px-4 py-8">
    <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
      <h1 class="text-2xl font-bold text-slate-900 mb-4">
        {{ t('home.create_title') }}
      </h1>

      <div
        v-if="!configStore.allowPublicEventCreation"
        data-testid="public-disabled-banner"
        class="rounded-xl bg-amber-50 border border-amber-200 p-4 text-amber-900 text-sm"
      >
        {{ t('home.public_disabled') }}
      </div>

      <form v-else data-testid="create-event-form" class="space-y-4" @submit.prevent="handleCreate">
        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1" for="event-title">
            {{ t('home.event_title') }} *
          </label>
          <input
            id="event-title"
            v-model="title"
            type="text"
            required
            :placeholder="t('home.event_title_placeholder')"
            class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-red-600 focus:outline-hidden"
          />
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1" for="org-first-name">
              {{ t('home.first_name') }} *
            </label>
            <input
              id="org-first-name"
              v-model="firstName"
              type="text"
              required
              class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-red-600 focus:outline-hidden"
            />
          </div>
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1" for="org-last-name">
              {{ t('home.last_name') }} *
            </label>
            <input
              id="org-last-name"
              v-model="lastName"
              type="text"
              required
              class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-red-600 focus:outline-hidden"
            />
          </div>
        </div>

        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1" for="org-email">
            {{ t('home.email') }} *
          </label>
          <input
            id="org-email"
            v-model="email"
            type="email"
            required
            class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-red-600 focus:outline-hidden"
          />
        </div>

        <p v-if="createError" class="text-sm text-red-600 font-medium">
          {{ createError }}
        </p>

        <button
          type="submit"
          :disabled="creating"
          class="w-full min-h-[44px] rounded-lg bg-red-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-red-700 disabled:opacity-50 cursor-pointer"
        >
          {{ creating ? t('home.creating') : t('home.create_btn') }}
        </button>
      </form>
    </section>

    <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
      <h2 class="text-lg font-bold text-slate-900 mb-2">
        {{ t('home.recover_title') }}
      </h2>
      <p class="text-sm text-slate-600 mb-4">
        {{ t('home.recover_desc') }}
      </p>

      <form data-testid="recover-form" class="flex flex-col sm:flex-row gap-3" @submit.prevent="handleRecover">
        <input
          v-model="recoverEmail"
          type="email"
          required
          :placeholder="t('home.email')"
          class="flex-1 min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm focus:border-red-600 focus:outline-hidden"
        />
        <button
          type="submit"
          class="min-h-[44px] rounded-lg bg-slate-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-slate-800 cursor-pointer"
        >
          {{ t('home.recover_btn') }}
        </button>
      </form>

      <p v-if="recoverMessage" data-testid="recover-message" class="mt-3 text-sm text-emerald-700 font-medium">
        {{ recoverMessage }}
      </p>
      <p v-if="recoverError" class="mt-3 text-sm text-red-600 font-medium">
        {{ recoverError }}
      </p>
    </section>
  </div>
</template>
