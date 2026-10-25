import { createI18n } from 'vue-i18n'
import fr from './fr.json'
import en from './en.json'

export type SupportedLocale = 'fr' | 'en'

export function detectInitialLocale(): SupportedLocale {
  try {
    const saved = localStorage.getItem('secret_santa_locale')
    if (saved === 'fr' || saved === 'en') {
      return saved
    }
  } catch {
    // ignore storage errors
  }
  if (typeof navigator !== 'undefined' && navigator.language) {
    if (navigator.language.toLowerCase().startsWith('en')) {
      return 'en'
    }
  }
  return 'fr'
}

export const i18n = createI18n({
  legacy: false,
  locale: detectInitialLocale(),
  fallbackLocale: 'fr',
  messages: {
    fr,
    en,
  },
})

export function setLocale(locale: SupportedLocale) {
  i18n.global.locale.value = locale
  try {
    localStorage.setItem('secret_santa_locale', locale)
  } catch {
    // ignore
  }
}
