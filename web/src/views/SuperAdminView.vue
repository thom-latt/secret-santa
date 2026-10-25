<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSuperAdminStore } from '../stores/superAdmin'
import DialogModal from '../components/DialogModal.vue'

const { t, locale } = useI18n()
const store = useSuperAdminStore()

const loginEmail = ref('')
const loginPassword = ref('')
const loginError = ref<string | null>(null)

const inviteTitle = ref('')
const inviteFirstName = ref('')
const inviteLastName = ref('')
const inviteEmail = ref('')
const statusMessage = ref<string | null>(null)
const actionError = ref<string | null>(null)

const deleteCandidateId = ref<string | null>(null)

onMounted(async () => {
  try {
    await store.fetchOverview()
  } catch {
    // not logged in yet
  }
})

async function handleLogin() {
  loginError.value = null
  try {
    await store.login(loginEmail.value, loginPassword.value)
    loginPassword.value = ''
  } catch (e: unknown) {
    loginError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleTogglePublic() {
  actionError.value = null
  try {
    await store.togglePublicCreation(!store.allowPublicEventCreation)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleTestSmtp() {
  actionError.value = null
  statusMessage.value = null
  try {
    const msg = await store.testSmtp()
    statusMessage.value = msg
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleInvite() {
  actionError.value = null
  statusMessage.value = null
  try {
    await store.inviteOrganizer({
      title: inviteTitle.value,
      first_name: inviteFirstName.value,
      last_name: inviteLastName.value,
      email: inviteEmail.value,
      locale: locale.value,
    })
    inviteTitle.value = ''
    inviteFirstName.value = ''
    inviteLastName.value = ''
    inviteEmail.value = ''
    statusMessage.value = t('superadmin.invite_sent')
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleResendAdmin(eventId: string) {
  actionError.value = null
  statusMessage.value = null
  try {
    await store.resendAdminLink(eventId)
    statusMessage.value = t('superadmin.admin_link_resent')
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

function formatDateShort(iso: string | null) {
  if (!iso) return ''
  return iso.slice(0, 10)
}

async function confirmDelete() {
  if (!deleteCandidateId.value) return
  const id = deleteCandidateId.value
  deleteCandidateId.value = null
  try {
    await store.deleteEvent(id)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="mx-auto max-w-5xl px-4 py-8 space-y-6">
    <!-- Login View -->
    <section
      v-if="!store.authenticated"
      class="mx-auto max-w-md rounded-2xl bg-white p-6 shadow-xs border border-slate-200"
    >
      <h1 class="text-xl font-bold text-slate-900 mb-4">{{ t('superadmin.login_title') }}</h1>
      <form data-testid="superadmin-login-form" class="space-y-4" @submit.prevent="handleLogin">
        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('admin.email') }}</label>
          <input
            v-model="loginEmail"
            type="email"
            required
            class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </div>
        <div>
          <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('superadmin.password') }}</label>
          <input
            v-model="loginPassword"
            type="password"
            required
            class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </div>
        <p v-if="loginError" class="text-sm text-red-600 font-medium">{{ loginError }}</p>
        <button
          type="submit"
          class="w-full min-h-[44px] rounded-lg bg-slate-900 px-4 py-2 text-sm font-semibold text-white hover:bg-slate-800 cursor-pointer"
        >
          {{ t('superadmin.login_btn') }}
        </button>
      </form>
    </section>

    <!-- Authenticated Back-Office -->
    <template v-else>
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
        <div>
          <h1 class="text-2xl font-bold text-slate-900">{{ t('superadmin.title') }}</h1>
          <p class="text-sm text-slate-600">{{ store.superAdminEmail }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            data-testid="test-smtp-btn"
            class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            @click="handleTestSmtp"
          >
            {{ t('superadmin.test_smtp_btn') }}
          </button>
          <button
            type="button"
            data-testid="superadmin-logout-btn"
            class="min-h-[44px] rounded-lg bg-slate-900 px-4 py-2 text-sm font-semibold text-white hover:bg-slate-800 cursor-pointer"
            @click="store.logout()"
          >
            {{ t('superadmin.logout_btn') }}
          </button>
        </div>
      </div>

      <p v-if="statusMessage" data-testid="superadmin-status-message" class="rounded-xl bg-emerald-50 border border-emerald-200 p-4 text-sm text-emerald-800 font-medium">
        {{ statusMessage }}
      </p>
      <p v-if="actionError" data-testid="superadmin-action-error" class="rounded-xl bg-red-50 border border-red-200 p-4 text-sm text-red-700 font-medium">
        {{ actionError }}
      </p>

      <!-- Settings Toggle (US-6.2) -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 flex items-center justify-between gap-4">
        <span class="text-sm font-medium text-slate-800">{{ t('superadmin.public_creation_toggle') }}</span>
        <button
          type="button"
          data-testid="toggle-public-creation"
          role="switch"
          :aria-checked="store.allowPublicEventCreation"
          class="min-h-[44px] min-w-[80px] rounded-lg px-4 py-2 text-xs font-bold uppercase tracking-wider text-white cursor-pointer transition-colors"
          :class="store.allowPublicEventCreation ? 'bg-emerald-600' : 'bg-slate-500'"
          @click="handleTogglePublic"
        >
          {{ store.allowPublicEventCreation ? 'ON' : 'OFF' }}
        </button>
      </section>

      <!-- Invite Organizer (US-6.3) -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-4">
        <h2 class="text-lg font-bold text-slate-900">{{ t('superadmin.invite_title') }}</h2>
        <form data-testid="invite-organizer-form" class="grid grid-cols-1 sm:grid-cols-5 gap-3" @submit.prevent="handleInvite">
          <input
            v-model="inviteTitle"
            type="text"
            required
            autocomplete="off"
            data-testid="invite-title"
            :placeholder="t('home.event_title')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            v-model="inviteFirstName"
            type="text"
            required
            autocomplete="off"
            data-testid="invite-first-name"
            :placeholder="t('admin.first_name')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            v-model="inviteLastName"
            type="text"
            required
            autocomplete="off"
            data-testid="invite-last-name"
            :placeholder="t('admin.last_name')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            v-model="inviteEmail"
            type="email"
            required
            autocomplete="off"
            data-testid="invite-email"
            :placeholder="t('admin.email')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <button
            type="submit"
            data-testid="invite-submit-btn"
            class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-700 cursor-pointer"
          >
            {{ t('superadmin.invite_btn') }}
          </button>
        </form>
      </section>

      <!-- Hosted Events List (US-6.4) -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-4">
        <h2 class="text-lg font-bold text-slate-900">
          {{ t('superadmin.events_title') }} ({{ store.events.length }})
        </h2>

        <p v-if="store.events.length === 0" data-testid="superadmin-events-empty" class="text-sm text-slate-600">
          {{ t('superadmin.no_events') }}
        </p>

        <div v-else class="space-y-3" data-testid="superadmin-events-list">
          <div
            v-for="ev in store.events"
            :key="ev.id"
            data-testid="superadmin-event-card"
            class="rounded-xl border border-slate-200 p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-4"
          >
            <div class="space-y-1">
              <div class="flex items-center gap-2">
                <span class="font-bold text-slate-900">{{ ev.title }}</span>
                <span
                  class="rounded-full px-2 py-0.5 text-xs font-semibold"
                  :class="ev.status === 'drawn' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
                >
                  {{ ev.status === 'drawn' ? t('admin.status_drawn') : t('admin.status_draft') }}
                </span>
              </div>
              <p class="text-sm text-slate-600">
                {{ ev.organizer_first_name }} {{ ev.organizer_last_name }} ({{ ev.organizer_email }})
              </p>
              <div class="flex flex-wrap gap-3 text-xs text-slate-600 pt-1">
                <span data-testid="event-created-at">{{ t('superadmin.col_created_at') }}: <strong>{{ formatDateShort(ev.created_at) }}</strong></span>
                <span data-testid="event-participants-count">{{ t('superadmin.col_participants') }}: <strong>{{ ev.participant_count }}</strong></span>
                <span data-testid="event-revealed-count">{{ t('superadmin.col_revealed') }}: <strong>{{ ev.revealed_targets_count }}/{{ ev.participant_count }}</strong></span>
                <span data-testid="event-wishlists-count">{{ t('superadmin.col_wishlists') }}: <strong>{{ ev.filled_wishlists_count }}/{{ ev.participant_count }}</strong></span>
              </div>
            </div>

            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                data-testid="resend-admin-link-btn"
                class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
                @click="handleResendAdmin(ev.id)"
              >
                {{ t('superadmin.resend_admin_link') }}
              </button>
              <button
                type="button"
                data-testid="superadmin-delete-event-btn"
                class="min-h-[44px] rounded-lg bg-red-600 px-3 py-2 text-xs font-semibold text-white hover:bg-red-700 cursor-pointer"
                @click="deleteCandidateId = ev.id"
              >
                {{ t('admin.delete_btn') }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <DialogModal
        :open="deleteCandidateId !== null"
        :title="t('admin.delete_confirm_title')"
        @close="deleteCandidateId = null"
      >
        <p>{{ t('admin.delete_confirm_desc') }}</p>
        <template #actions>
          <button
            type="button"
            data-testid="superadmin-cancel-delete-btn"
            class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 cursor-pointer"
            @click="deleteCandidateId = null"
          >
            {{ t('admin.cancel_btn') }}
          </button>
          <button
            type="button"
            data-testid="superadmin-confirm-delete-btn"
            class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white cursor-pointer"
            @click="confirmDelete"
          >
            {{ t('admin.delete_btn') }}
          </button>
        </template>
      </DialogModal>
    </template>
  </div>
</template>
