<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppConfigStore } from '../stores/appConfig'

const { t } = useI18n()
const config = useAppConfigStore()

onMounted(() => {
  // The operator identity is optional decoration: the static text still renders if the fetch fails.
  config.fetchPublicConfig().catch(() => {})
})

const sections = ['purpose', 'data', 'recipients', 'retention', 'access'] as const
</script>

<template>
  <div class="mx-auto max-w-2xl px-4 py-8 space-y-6">
    <h1 class="text-2xl font-extrabold text-slate-900">{{ t('privacy.title') }}</h1>

    <section class="space-y-1">
      <h2 class="text-lg font-bold text-slate-900">{{ t('privacy.controller_title') }}</h2>
      <p class="text-sm text-slate-700">
        {{ t('privacy.controller', { name: config.operatorName, email: config.operatorEmail }) }}
      </p>
    </section>

    <section v-for="key in sections" :key="key" class="space-y-1">
      <h2 class="text-lg font-bold text-slate-900">{{ t(`privacy.${key}_title`) }}</h2>
      <p class="text-sm text-slate-700">{{ t(`privacy.${key}`) }}</p>
    </section>

    <section class="space-y-1">
      <h2 class="text-lg font-bold text-slate-900">{{ t('privacy.rights_title') }}</h2>
      <p class="text-sm text-slate-700">
        {{ t('privacy.rights', { email: config.operatorEmail }) }}
      </p>
    </section>

    <p>
      <a
        v-if="config.operatorEmail"
        :href="`mailto:${config.operatorEmail}`"
        class="text-sm font-semibold text-red-700 underline"
      >{{ config.operatorEmail }}</a>
    </p>
  </div>
</template>
