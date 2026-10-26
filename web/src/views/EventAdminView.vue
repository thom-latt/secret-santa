<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useEventAdminStore, type AdminParticipant } from '../stores/eventAdmin'
import DialogModal from '../components/DialogModal.vue'
import ErrorView from './ErrorView.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useEventAdminStore()

const adminToken = computed(() => String(route.params.token || ''))

const editTitle = ref('')
const editDate = ref('')
const editBudget = ref('')
const editDescription = ref('')
const editOrganizerParticipates = ref(true)
const detailsSaved = ref(false)
const actionError = ref<string | null>(null)

const copied = ref(false)

// Add participant form
const firstNameInput = ref<HTMLInputElement | null>(null)
const newFirstName = ref('')
const newLastName = ref('')
const newEmail = ref('')

// Edit participant inline state
const editingParticipantId = ref<string | null>(null)
const editPartFirstName = ref('')
const editPartLastName = ref('')
const editPartEmail = ref('')

// Exclusions form
const exPartA = ref('')
const exPartB = ref('')

// Modals
const showDrawModal = ref(false)
const showDeleteModal = ref(false)
const drawing = ref(false)

function syncFormFromEvent() {
  if (!store.event) return
  editTitle.value = store.event.title
  editDate.value = store.event.event_date
  editBudget.value = store.event.budget
  editDescription.value = store.event.description
  editOrganizerParticipates.value = store.event.organizer_participates
}

watch(() => store.event, syncFormFromEvent)

onMounted(async () => {
  await store.fetchEvent(adminToken.value)
  syncFormFromEvent()
})

const hasFailedEmails = computed(() => {
  if (!store.event || store.event.status !== 'drawn') return false
  return store.event.participants.some((p) => !p.email_sent_at)
})

const canLaunchDraw = computed(() => {
  if (!store.event || store.event.status !== 'draft') return false
  return (
    store.event.participants.length >= 3 &&
    store.event.feasibility.mode !== 'impossible' &&
    store.event.feasibility.mode !== 'too_few'
  )
})

function participantName(id: string): string {
  const p = store.event?.participants.find((x) => x.id === id)
  return p ? `${p.first_name} ${p.last_name}` : id
}

function formatDate(iso: string): string {
  return iso.slice(0, 10)
}

function isPairExcluded(idA: string, idB: string): boolean {
  if (!idA || !idB || !store.event) return false
  return store.event.exclusions.some(
    (ex) =>
      (ex.participant_a_id === idA && ex.participant_b_id === idB) ||
      (ex.participant_a_id === idB && ex.participant_b_id === idA),
  )
}

async function copyAdminLink() {
  try {
    await navigator.clipboard.writeText(window.location.href)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    // ignore clipboard errors
  }
}

async function handleSaveDetails() {
  actionError.value = null
  detailsSaved.value = false
  try {
    await store.updateDetails(adminToken.value, {
      title: editTitle.value,
      description: editDescription.value,
      event_date: editDate.value,
      budget: editBudget.value,
      organizer_participates: editOrganizerParticipates.value,
    })
    detailsSaved.value = true
    setTimeout(() => {
      detailsSaved.value = false
    }, 2000)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleToggleOrganizerParticipates() {
  await handleSaveDetails()
}

async function handleAddParticipant() {
  actionError.value = null
  try {
    await store.addParticipant(adminToken.value, {
      first_name: newFirstName.value,
      last_name: newLastName.value,
      email: newEmail.value,
    })
    newFirstName.value = ''
    newLastName.value = ''
    newEmail.value = ''
    firstNameInput.value?.focus()
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

function startEditParticipant(p: AdminParticipant) {
  editingParticipantId.value = p.id
  editPartFirstName.value = p.first_name
  editPartLastName.value = p.last_name
  editPartEmail.value = p.email
}

async function saveEditParticipant(participantId: string) {
  actionError.value = null
  try {
    await store.updateParticipant(adminToken.value, participantId, {
      first_name: editPartFirstName.value,
      last_name: editPartLastName.value,
      email: editPartEmail.value,
    })
    editingParticipantId.value = null
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleDeleteParticipant(participantId: string) {
  actionError.value = null
  try {
    await store.deleteParticipant(adminToken.value, participantId)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleAddExclusion() {
  actionError.value = null
  if (!exPartA.value || !exPartB.value || exPartA.value === exPartB.value) return
  try {
    await store.addExclusion(adminToken.value, exPartA.value, exPartB.value)
    exPartA.value = ''
    exPartB.value = ''
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleDeleteExclusion(exclusionId: string) {
  actionError.value = null
  try {
    await store.deleteExclusion(adminToken.value, exclusionId)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function confirmAndExecuteDraw() {
  if (drawing.value) return
  actionError.value = null
  drawing.value = true
  showDrawModal.value = false
  try {
    // Save any pending edits in event details first (US-3.1)
    await store.updateDetails(adminToken.value, {
      title: editTitle.value,
      description: editDescription.value,
      event_date: editDate.value,
      budget: editBudget.value,
      organizer_participates: editOrganizerParticipates.value,
    })
    await store.executeDraw(adminToken.value)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  } finally {
    drawing.value = false
  }
}

async function handleResendSingle(participantId: string) {
  actionError.value = null
  try {
    await store.resendParticipantEmail(adminToken.value, participantId)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function handleResendFailedAll() {
  actionError.value = null
  try {
    await store.resendFailedEmails(adminToken.value)
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function confirmAndDeleteEvent() {
  showDeleteModal.value = false
  try {
    await store.deleteEvent(adminToken.value)
    await router.push('/')
  } catch (e: unknown) {
    actionError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl px-4 py-8 space-y-6">
    <ErrorView v-if="store.notFound" :status="404" />
    <ErrorView
      v-else-if="store.error && !store.event"
      :status="500"
      :on-retry="() => store.fetchEvent(adminToken)"
    />

    <template v-else-if="store.event">
      <!-- Top Bar -->
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
        <div>
          <div class="flex items-center gap-2 mb-1">
            <span
              class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold"
              :class="store.event.status === 'drawn' ? 'bg-emerald-100 text-emerald-800' : 'bg-amber-100 text-amber-800'"
            >
              {{ store.event.status === 'drawn' ? t('admin.status_drawn') : t('admin.status_draft') }}
            </span>
          </div>
          <h1 class="text-2xl font-bold text-slate-900">{{ store.event.title }}</h1>
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="min-h-[44px] rounded-lg border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
            @click="copyAdminLink"
          >
            {{ copied ? t('admin.link_copied') : t('admin.copy_link') }}
          </button>

          <router-link
            v-if="store.event.status === 'drawn' && store.event.organizer_participates && store.event.organizer_participant_token"
            data-testid="organizer-participant-link"
            :to="`/p/${store.event.organizer_participant_token}`"
            class="inline-flex items-center min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-700"
          >
            {{ t('admin.go_to_my_page') }}
          </router-link>
        </div>
      </div>

      <p v-if="actionError" class="rounded-xl bg-red-50 border border-red-200 p-4 text-sm text-red-700 font-medium">
        {{ actionError }}
      </p>

      <!-- Event Details Form -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200">
        <h2 class="text-lg font-bold text-slate-900 mb-4">{{ t('admin.details_title') }}</h2>
        <form class="space-y-4" @submit.prevent="handleSaveDetails">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div class="sm:col-span-1">
              <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('home.event_title') }} *</label>
              <input
                v-model="editTitle"
                type="text"
                required
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('admin.event_date') }}</label>
              <input
                v-model="editDate"
                type="date"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('admin.budget') }}</label>
              <input
                v-model="editBudget"
                type="text"
                :placeholder="t('admin.budget_placeholder')"
                class="w-full min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">{{ t('admin.description') }}</label>
            <textarea
              v-model="editDescription"
              rows="2"
              class="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
            />
          </div>

          <div v-if="store.event.status === 'draft'" class="flex items-center gap-3 min-h-[44px]">
            <input
              id="org-participates"
              v-model="editOrganizerParticipates"
              type="checkbox"
              class="h-5 w-5 rounded border-slate-300 text-red-600"
              @change="handleToggleOrganizerParticipates"
            />
            <label for="org-participates" class="text-sm font-medium text-slate-800 cursor-pointer">
              {{ t('admin.organizer_participates') }}
            </label>
          </div>

          <div class="flex items-center gap-3">
            <button
              type="submit"
              class="min-h-[44px] rounded-lg bg-slate-900 px-4 py-2 text-sm font-semibold text-white hover:bg-slate-800 cursor-pointer"
            >
              {{ t('admin.save_details') }}
            </button>
            <span v-if="detailsSaved" class="text-sm text-emerald-700 font-medium">{{ t('admin.saved') }}</span>
          </div>
        </form>
      </section>

      <!-- Participants Section -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-4">
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <h2 class="text-lg font-bold text-slate-900">
            {{ t('admin.participants_title') }} ({{ store.event.participants.length }})
          </h2>
          <button
            v-if="hasFailedEmails"
            type="button"
            data-testid="resend-failed-btn"
            class="min-h-[44px] rounded-lg bg-amber-600 px-4 py-2 text-sm font-semibold text-white hover:bg-amber-700 cursor-pointer"
            @click="handleResendFailedAll"
          >
            {{ t('admin.resend_failed_all') }}
          </button>
        </div>

        <!-- Add Participant Form (Draft only) -->
        <form
          v-if="store.event.status === 'draft'"
          data-testid="add-participant-form"
          class="grid grid-cols-1 sm:grid-cols-4 gap-3 pt-2"
          @submit.prevent="handleAddParticipant"
        >
          <input
            ref="firstNameInput"
            v-model="newFirstName"
            type="text"
            required
            autocomplete="off"
            :placeholder="t('admin.first_name')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            v-model="newLastName"
            type="text"
            required
            autocomplete="off"
            :placeholder="t('admin.last_name')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <input
            v-model="newEmail"
            type="email"
            required
            autocomplete="off"
            :placeholder="t('admin.email')"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
          <button
            type="submit"
            class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-700 cursor-pointer"
          >
            {{ t('admin.add_btn') }}
          </button>
        </form>

        <!-- Participant Cards (Mobile-first stacked cards) -->
        <div class="space-y-3">
          <div
            v-for="p in store.event.participants"
            :key="p.id"
            class="rounded-xl border border-slate-200 p-4 flex flex-col gap-3"
          >
            <div v-if="editingParticipantId === p.id" class="grid grid-cols-1 sm:grid-cols-4 gap-2">
              <input
                v-model="editPartFirstName"
                data-testid="edit-participant-first-name"
                type="text"
                class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <input
                v-model="editPartLastName"
                data-testid="edit-participant-last-name"
                type="text"
                class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <input
                v-model="editPartEmail"
                data-testid="edit-participant-email"
                type="email"
                class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm"
              />
              <div class="flex gap-2">
                <button
                  type="button"
                  data-testid="save-participant-btn"
                  class="min-h-[44px] flex-1 rounded-lg bg-emerald-600 px-3 py-2 text-xs font-semibold text-white cursor-pointer"
                  @click="saveEditParticipant(p.id)"
                >
                  {{ t('admin.save_btn') }}
                </button>
                <button
                  type="button"
                  data-testid="cancel-participant-btn"
                  class="min-h-[44px] flex-1 rounded-lg border border-slate-300 px-3 py-2 text-xs font-medium text-slate-700 cursor-pointer"
                  @click="editingParticipantId = null"
                >
                  {{ t('admin.cancel_btn') }}
                </button>
              </div>
            </div>

            <div v-else class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div>
                <div class="flex items-center gap-2">
                  <span class="font-semibold text-slate-900">{{ p.first_name }} {{ p.last_name }}</span>
                  <span
                    v-if="p.is_organizer"
                    class="rounded-full bg-slate-100 px-2.5 py-0.5 text-xs font-medium text-slate-700"
                  >
                    {{ t('admin.organizer_badge') }}
                  </span>
                </div>
                <p class="text-sm text-slate-600">{{ p.email }}</p>

                <!-- Post-draw tracking indicators (US-4.1) -->
                <div v-if="store.event.status === 'drawn'" class="mt-2 flex flex-wrap gap-2 text-xs">
                  <span
                    data-testid="indicator-email"
                    class="inline-flex items-center gap-1 rounded-md px-2 py-1 font-medium"
                    :class="p.email_sent_at ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-700'"
                  >
                    <span aria-hidden="true">{{ p.email_sent_at ? '✓' : '✕' }}</span>
                    <span>
                      {{ t('admin.tracking_email') }}:
                      {{ p.email_sent_at ? t('admin.email_sent') : t('admin.email_failed') }}
                    </span>
                  </span>
                  <span
                    data-testid="indicator-revealed"
                    class="inline-flex items-center gap-1 rounded-md px-2 py-1 font-medium"
                    :class="p.link_clicked_at ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-700'"
                  >
                    <span aria-hidden="true">{{ p.link_clicked_at ? '👁️' : '⏳' }}</span>
                    <span>
                      {{ t('admin.tracking_revealed') }}:
                      {{ p.link_clicked_at ? t('admin.revealed_yes_date', { date: formatDate(p.link_clicked_at) }) : t('admin.revealed_pending') }}
                    </span>
                  </span>
                  <span
                    data-testid="indicator-wishlist"
                    class="inline-flex items-center gap-1 rounded-md px-2 py-1 font-medium"
                    :class="p.wishlist_count > 0 ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-700'"
                  >
                    <span aria-hidden="true">{{ p.wishlist_count > 0 ? '🎁' : '📋' }}</span>
                    <span>
                      {{ t('admin.tracking_wishlist') }}:
                      {{ p.wishlist_count > 0 ? t('admin.wishlist_yes', { count: p.wishlist_count }) : t('admin.wishlist_empty') }}
                    </span>
                  </span>
                </div>
              </div>

              <div class="flex flex-wrap gap-2">
                <button
                  type="button"
                  data-testid="edit-participant-btn"
                  class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer"
                  @click="startEditParticipant(p)"
                >
                  {{ t('admin.edit_btn') }}
                </button>
                <button
                  v-if="store.event.status === 'draft'"
                  type="button"
                  data-testid="delete-participant-btn"
                  class="min-h-[44px] rounded-lg border border-red-200 px-3 py-2 text-xs font-medium text-red-700 hover:bg-red-50 cursor-pointer"
                  @click="handleDeleteParticipant(p.id)"
                >
                  {{ t('admin.delete_btn') }}
                </button>
                <button
                  v-if="store.event.status === 'drawn'"
                  type="button"
                  data-testid="resend-participant-btn"
                  class="min-h-[44px] rounded-lg bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 cursor-pointer"
                  @click="handleResendSingle(p.id)"
                >
                  {{ t('admin.resend_email') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Mutual Exclusions & Feasibility (Draft Mode) -->
      <section v-if="store.event.status === 'draft'" class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-4">
        <div>
          <h2 class="text-lg font-bold text-slate-900">{{ t('admin.exclusions_title') }}</h2>
          <p class="text-sm text-slate-600">{{ t('admin.exclusions_desc') }}</p>
        </div>

        <!-- Dry-Run Feasibility Indicator (US-2.5) -->
        <div
          data-testid="feasibility-badge"
          class="rounded-xl p-3 text-sm font-medium border"
          :class="{
            'bg-emerald-50 border-emerald-200 text-emerald-800': store.event.feasibility.mode === 'single_cycle',
            'bg-blue-50 border-blue-200 text-blue-800': store.event.feasibility.mode === 'sub_loops',
            'bg-red-50 border-red-200 text-red-800': store.event.feasibility.mode === 'impossible',
            'bg-slate-50 border-slate-200 text-slate-700': store.event.feasibility.mode === 'too_few',
          }"
        >
          <span v-if="store.event.feasibility.mode === 'single_cycle'">{{ t('admin.feasibility_single_cycle') }}</span>
          <span v-else-if="store.event.feasibility.mode === 'sub_loops'">{{ t('admin.feasibility_sub_loops') }}</span>
          <span v-else-if="store.event.feasibility.mode === 'impossible'">
            {{ t('admin.feasibility_impossible') }} — {{ store.event.feasibility.diagnostic }}
          </span>
          <span v-else>{{ t('admin.feasibility_too_few') }} ({{ store.event.feasibility.diagnostic }})</span>
        </div>

        <form data-testid="add-exclusion-form" class="grid grid-cols-1 sm:grid-cols-3 gap-3" @submit.prevent="handleAddExclusion">
          <select
            v-model="exPartA"
            data-testid="exclusion-select-a"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm bg-white"
          >
            <option value="">{{ t('admin.select_participant_a') }}</option>
            <option
              v-for="p in store.event.participants"
              :key="p.id"
              :value="p.id"
              :disabled="p.id === exPartB || isPairExcluded(p.id, exPartB)"
            >
              {{ p.first_name }} {{ p.last_name }}
            </option>
          </select>

          <select
            v-model="exPartB"
            data-testid="exclusion-select-b"
            class="min-h-[44px] rounded-lg border border-slate-300 px-3 py-2 text-sm bg-white"
          >
            <option value="">{{ t('admin.select_participant_b') }}</option>
            <option
              v-for="p in store.event.participants"
              :key="p.id"
              :value="p.id"
              :disabled="p.id === exPartA || isPairExcluded(exPartA, p.id)"
            >
              {{ p.first_name }} {{ p.last_name }}
            </option>
          </select>

          <button
            type="submit"
            data-testid="add-exclusion-btn"
            :disabled="!exPartA || !exPartB || exPartA === exPartB || isPairExcluded(exPartA, exPartB)"
            class="min-h-[44px] rounded-lg bg-slate-900 px-4 py-2 text-sm font-semibold text-white hover:bg-slate-800 disabled:opacity-50 cursor-pointer"
          >
            {{ t('admin.add_exclusion_btn') }}
          </button>
        </form>

        <div v-if="store.event.exclusions.length > 0" class="space-y-2">
          <div
            v-for="ex in store.event.exclusions"
            :key="ex.id"
            class="flex items-center justify-between rounded-lg border border-slate-200 px-3 py-2 text-sm"
          >
            <span>{{ participantName(ex.participant_a_id) }} ↔ {{ participantName(ex.participant_b_id) }}</span>
            <button
              type="button"
              class="min-h-[44px] px-3 text-xs font-medium text-red-700 hover:underline cursor-pointer"
              @click="handleDeleteExclusion(ex.id)"
            >
              {{ t('admin.delete_btn') }}
            </button>
          </div>
        </div>
      </section>

      <!-- Launch Draw Action (Draft Mode) -->
      <section v-if="store.event.status === 'draft'" class="rounded-2xl bg-white p-6 shadow-xs border border-slate-200 space-y-3">
        <p
          v-if="store.event.participants.length < 3"
          data-testid="draw-need-more-msg"
          class="text-sm font-medium text-slate-600"
        >
          {{ t('admin.draw_need_more', { count: 3 - store.event.participants.length }) }}
        </p>
        <button
          type="button"
          data-testid="launch-draw-btn"
          :disabled="!canLaunchDraw || drawing"
          class="w-full min-h-[44px] rounded-xl bg-red-600 px-6 py-3 text-base font-bold text-white hover:bg-red-700 disabled:opacity-50 cursor-pointer"
          @click="showDrawModal = true"
        >
          {{ drawing ? t('admin.drawing') : t('admin.draw_btn') }}
        </button>
      </section>

      <!-- Danger Zone: Delete Event -->
      <section class="rounded-2xl bg-white p-6 shadow-xs border border-red-200 flex justify-between items-center">
        <span class="text-sm font-medium text-slate-700">{{ t('admin.delete_event') }}</span>
        <button
          type="button"
          data-testid="delete-event-btn"
          class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-700 cursor-pointer"
          @click="showDeleteModal = true"
        >
          {{ t('admin.delete_event') }}
        </button>
      </section>

      <!-- Draw Confirmation Modal -->
      <DialogModal :open="showDrawModal" :title="t('admin.draw_confirm_title')" @close="showDrawModal = false">
        <p>{{ t('admin.draw_confirm_desc') }}</p>
        <template #actions>
          <button
            type="button"
            class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 cursor-pointer"
            @click="showDrawModal = false"
          >
            {{ t('admin.cancel_btn') }}
          </button>
          <button
            type="button"
            data-testid="confirm-draw-btn"
            class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white cursor-pointer"
            @click="confirmAndExecuteDraw"
          >
            {{ t('admin.draw_confirm_btn') }}
          </button>
        </template>
      </DialogModal>

      <!-- Delete Confirmation Modal -->
      <DialogModal :open="showDeleteModal" :title="t('admin.delete_confirm_title')" @close="showDeleteModal = false">
        <p>{{ t('admin.delete_confirm_desc') }}</p>
        <template #actions>
          <button
            type="button"
            class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 cursor-pointer"
            @click="showDeleteModal = false"
          >
            {{ t('admin.cancel_btn') }}
          </button>
          <button
            type="button"
            data-testid="confirm-delete-btn"
            class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white cursor-pointer"
            @click="confirmAndDeleteEvent"
          >
            {{ t('admin.delete_btn') }}
          </button>
        </template>
      </DialogModal>
    </template>
  </div>
</template>
