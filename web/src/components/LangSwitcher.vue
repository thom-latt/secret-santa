<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { setLocale, type SupportedLocale } from '../i18n'

const { locale } = useI18n()

function switchLang(lang: SupportedLocale) {
  setLocale(lang)
}
</script>

<template>
  <div class="inline-flex rounded-lg border border-slate-200 bg-white p-0.5 shadow-2xs" role="group" aria-label="Language switcher">
    <button
      type="button"
      data-testid="lang-fr"
      class="min-h-[44px] min-w-[44px] px-3 py-1.5 text-sm font-medium rounded-md transition-colors cursor-pointer"
      :class="locale === 'fr' ? 'bg-red-600 text-white' : 'text-slate-700 hover:bg-slate-100'"
      @click="switchLang('fr')"
    >
      FR
    </button>
    <button
      type="button"
      data-testid="lang-en"
      class="min-h-[44px] min-w-[44px] px-3 py-1.5 text-sm font-medium rounded-md transition-colors cursor-pointer"
      :class="locale === 'en' ? 'bg-red-600 text-white' : 'text-slate-700 hover:bg-slate-100'"
      @click="switchLang('en')"
    >
      EN
    </button>
  </div>
</template>
