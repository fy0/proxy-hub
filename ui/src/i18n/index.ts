import dayjs from 'dayjs';
import 'dayjs/locale/en';
import 'dayjs/locale/zh-cn';
import { readonly, ref } from 'vue';
import { fallbackLocale, messages, type Locale } from './messages';

type MessageParams = Record<string, number | string>;

const currentLocale = ref<Locale>(detectLocale());
let initialized = false;
let languageChangeListenerAttached = false;

function normalizeLocaleTag(value: string | null | undefined): Locale | null {
  const tag = value?.trim().replace('_', '-').toLowerCase();
  if (!tag) return null;

  if (tag.startsWith('zh')) return 'zh-CN';
  if (tag.startsWith('en')) return 'en-US';

  return null;
}

function systemLanguageCandidates(): string[] {
  const candidates: string[] = [];

  if (typeof navigator !== 'undefined') {
    if (Array.isArray(navigator.languages)) {
      candidates.push(...navigator.languages);
    }

    candidates.push(navigator.language);
  }

  if (typeof Intl !== 'undefined') {
    candidates.push(Intl.DateTimeFormat().resolvedOptions().locale);
  }

  return candidates.filter(Boolean);
}

export function detectLocale(): Locale {
  for (const candidate of systemLanguageCandidates()) {
    const locale = normalizeLocaleTag(candidate);
    if (locale) return locale;
  }

  return fallbackLocale;
}

function applyLocale(nextLocale: Locale): void {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = nextLocale;
    document.documentElement.dataset.locale = nextLocale;
  }

  dayjs.locale(nextLocale === 'zh-CN' ? 'zh-cn' : 'en');
}

export function setLocale(nextLocale: Locale): void {
  if (currentLocale.value !== nextLocale) {
    currentLocale.value = nextLocale;
  }

  applyLocale(nextLocale);
}

export function syncLocaleWithSystem(): void {
  setLocale(detectLocale());
}

function onLanguageChange(): void {
  syncLocaleWithSystem();
}

export function initI18n(): Locale {
  syncLocaleWithSystem();

  if (!languageChangeListenerAttached && typeof window !== 'undefined') {
    window.addEventListener('languagechange', onLanguageChange);
    languageChangeListenerAttached = true;
  }

  initialized = true;
  return currentLocale.value;
}

function resolveMessage(locale: Locale, key: string): string | undefined {
  const value = key.split('.').reduce<unknown>((target, part) => {
    if (!target || typeof target !== 'object') return undefined;

    return (target as Record<string, unknown>)[part];
  }, messages[locale]);

  return typeof value === 'string' ? value : undefined;
}

function interpolate(template: string, params?: MessageParams): string {
  if (!params) return template;

  return template.replace(/\{(\w+)\}/g, (match, key: string) => {
    const value = params[key];
    return value === undefined ? match : String(value);
  });
}

export function t(key: string, params?: MessageParams): string {
  const template =
    resolveMessage(currentLocale.value, key) ?? resolveMessage(fallbackLocale, key) ?? key;
  return interpolate(template, params);
}

export function formatDateTime(
  value: Date | number | string,
  options?: Intl.DateTimeFormatOptions
): string {
  const date = value instanceof Date ? value : new Date(value);
  return new Intl.DateTimeFormat(currentLocale.value, options).format(date);
}

export function useI18n() {
  if (!initialized) {
    initI18n();
  }

  return {
    formatDateTime,
    locale: readonly(currentLocale),
    setLocale,
    t,
  };
}

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    if (languageChangeListenerAttached && typeof window !== 'undefined') {
      window.removeEventListener('languagechange', onLanguageChange);
    }
  });
}
