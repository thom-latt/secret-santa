<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    status?: 404 | 500
    onRetry?: () => void
  }>(),
  { status: 404 },
)

const { t } = useI18n()
const is5xx = computed(() => props.status >= 500)
</script>

<template>
  <div class="mx-auto max-w-xl px-4 py-12">
    <div
      :data-testid="is5xx ? 'server-error' : 'not-found'"
      class="rounded-2xl bg-white p-8 text-center shadow-xs border border-slate-200 space-y-4"
    >
      <div
        class="mx-auto inline-flex items-center rounded-full px-3 py-1 text-xs font-bold uppercase tracking-wider"
        :class="is5xx ? 'bg-red-100 text-red-800' : 'bg-amber-100 text-amber-800'"
      >
        {{ is5xx ? t('common.error_5xx_code') : t('common.error_4xx_code') }}
      </div>

      <h1 class="text-xl font-bold text-slate-900">
        {{ is5xx ? t('common.server_error') : t('common.not_found') }}
      </h1>

      <p class="text-sm text-slate-600">
        {{ is5xx ? t('common.server_error_desc') : t('common.not_found_desc') }}
      </p>

      <div class="pt-2 flex flex-wrap items-center justify-center gap-3">
        <button
          v-if="is5xx && props.onRetry"
          type="button"
          data-testid="error-retry-btn"
          class="inline-flex items-center min-h-[44px] px-4 py-2 rounded-lg border border-slate-300 bg-white text-slate-700 text-sm font-semibold hover:bg-slate-50 cursor-pointer"
          @click="props.onRetry()"
        >
          {{ t('common.retry') }}
        </button>

        <router-link
          to="/"
          class="inline-flex items-center min-h-[44px] px-4 py-2 rounded-lg bg-red-600 text-white text-sm font-semibold hover:bg-red-700"
        >
          {{ t('common.back_home') }}
        </router-link>
      </div>
    </div>
  </div>
</template>
