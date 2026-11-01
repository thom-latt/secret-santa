<script setup lang="ts">
import { ref, computed, onMounted, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import DialogModal from './DialogModal.vue'
import { useAppConfigStore } from '../stores/appConfig'
import { sendFeedback, type FeedbackKind, type FeedbackScope } from '../stores/feedback'

const props = defineProps<{ scope: FeedbackScope; token: string }>()

const MAX_LEN = 2000
const { t } = useI18n()
const config = useAppConfigStore()
const uid = useId()
const formId = `feedback-form-${uid}`

const open = ref(false)
const kind = ref<FeedbackKind>('feedback')
const message = ref('')
const sending = ref(false)
const error = ref<string | null>(null)
const sent = ref(false)

const trimmedLength = computed(() => [...message.value.trim()].length)

// The public config is only fetched by the home page; these views may be the entry point.
onMounted(async () => {
  if (config.loaded) return
  try {
    await config.fetchPublicConfig()
  } catch {
    // button simply stays hidden
  }
})

function openDialog() {
  error.value = null
  open.value = true
}

async function submit() {
  if (sending.value) return
  if (trimmedLength.value < 1 || trimmedLength.value > MAX_LEN) {
    error.value = t('errors.invalid_feedback')
    return
  }
  error.value = null
  sending.value = true
  try {
    await sendFeedback(props.scope, props.token, { kind: kind.value, message: message.value.trim() })
    message.value = ''
    open.value = false
    sent.value = true
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <div v-if="config.operatorEmail" class="mt-8 text-center">
    <p v-if="sent" data-testid="feedback-success" role="status" class="text-sm text-green-700">
      {{ t('feedback.success') }}
    </p>
    <button
      type="button"
      data-testid="feedback-open-btn"
      class="min-h-[44px] text-sm text-slate-600 underline cursor-pointer"
      @click="openDialog"
    >
      {{ t('feedback.open_btn') }}
    </button>

    <DialogModal :open="open" :title="t('feedback.title')" @close="open = false">
      <form :id="formId" data-testid="feedback-form" class="text-left space-y-3" @submit.prevent="submit">
        <div>
          <label :for="`${formId}-kind`" class="block text-sm font-medium text-slate-700">{{ t('feedback.kind_label') }}</label>
          <select
            :id="`${formId}-kind`"
            v-model="kind"
            class="mt-1 w-full min-h-[44px] rounded-lg border border-slate-300 px-3"
          >
            <option value="bug">{{ t('feedback.kind_bug') }}</option>
            <option value="feedback">{{ t('feedback.kind_feedback') }}</option>
            <option value="idea">{{ t('feedback.kind_idea') }}</option>
          </select>
        </div>
        <div>
          <label :for="`${formId}-message`" class="block text-sm font-medium text-slate-700">{{ t('feedback.message_label') }}</label>
          <textarea
            :id="`${formId}-message`"
            v-model="message"
            rows="5"
            class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2"
          />
          <p class="text-xs text-slate-500 text-right">{{ trimmedLength }} / {{ MAX_LEN }}</p>
        </div>
        <p class="text-xs text-slate-500">{{ t('feedback.reply_note') }}</p>
        <p v-if="error" role="alert" class="text-sm text-red-700">{{ error }}</p>
      </form>
      <template #actions>
        <button
          type="button"
          class="min-h-[44px] rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 cursor-pointer"
          @click="open = false"
        >
          {{ t('feedback.cancel_btn') }}
        </button>
        <button
          type="submit"
          :form="formId"
          :disabled="sending"
          :aria-busy="sending"
          class="min-h-[44px] rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white cursor-pointer disabled:opacity-50"
        >
          {{ t('feedback.send_btn') }}
        </button>
      </template>
    </DialogModal>
  </div>
</template>
