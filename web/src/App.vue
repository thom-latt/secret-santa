<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LangSwitcher from './components/LangSwitcher.vue'

const { t } = useI18n()
</script>

<template>
  <div class="min-h-screen flex flex-col bg-slate-50 text-slate-900">
    <header class="border-b border-slate-200 bg-white">
      <div class="mx-auto max-w-5xl px-4 py-3 flex items-center justify-between gap-4">
        <router-link to="/" class="flex items-center gap-2 font-extrabold text-lg text-slate-900">
          <span aria-hidden="true">🎅</span>
          <span>{{ t('app.title') }}</span>
        </router-link>
        <LangSwitcher />
      </div>
    </header>

    <main class="flex-1">
      <router-view />
    </main>
  </div>
</template>
