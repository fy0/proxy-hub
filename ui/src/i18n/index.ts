import dayjs from 'dayjs';
import 'dayjs/locale/en';
import 'dayjs/locale/zh-cn';
import { readonly, ref } from 'vue';
import { fallbackLocale, messages, type Locale } from './messages';

type MessageParams = Record<string, number | string>;
export type LocalePreference = 'system' | Locale;

const LOCALE_PREFERENCE_STORAGE_KEY = 'proxy_hub_locale_preference';
const defaultLocalePreference: LocalePreference = 'system';

const currentLocalePreference = ref<LocalePreference>(readStoredLocalePreference());
const currentLocale = ref<Locale>(resolveLocalePreference(currentLocalePreference.value));
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

function normalizeLocalePreference(value: string | null | undefined): LocalePreference | null {
  const preference = value?.trim();
  if (!preference) return null;
  if (preference === 'system') return 'system';

  return normalizeLocaleTag(preference);
}

function readStoredLocalePreference(): LocalePreference {
  if (typeof localStorage === 'undefined') return defaultLocalePreference;

  try {
    return (
      normalizeLocalePreference(localStorage.getItem(LOCALE_PREFERENCE_STORAGE_KEY)) ??
      defaultLocalePreference
    );
  } catch {
    return defaultLocalePreference;
  }
}

function writeStoredLocalePreference(nextPreference: LocalePreference): void {
  if (typeof localStorage === 'undefined') return;

  try {
    localStorage.setItem(LOCALE_PREFERENCE_STORAGE_KEY, nextPreference);
  } catch {
    // Storage may be unavailable in hardened browser modes.
  }
}

function resolveLocalePreference(preference: LocalePreference): Locale {
  return preference === 'system' ? detectLocale() : preference;
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

function applyLocalePreference(): void {
  setLocale(resolveLocalePreference(currentLocalePreference.value));
}

export function setLocalePreference(nextPreference: LocalePreference): void {
  const normalizedPreference = normalizeLocalePreference(nextPreference) ?? defaultLocalePreference;
  if (currentLocalePreference.value !== normalizedPreference) {
    currentLocalePreference.value = normalizedPreference;
  }

  writeStoredLocalePreference(normalizedPreference);
  applyLocalePreference();
}

export function syncLocaleWithSystem(): void {
  setLocale(detectLocale());
}

function onLanguageChange(): void {
  if (currentLocalePreference.value === 'system') {
    syncLocaleWithSystem();
  }
}

export function initI18n(): Locale {
  currentLocalePreference.value = readStoredLocalePreference();
  applyLocalePreference();

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
    localePreference: readonly(currentLocalePreference),
    setLocale,
    setLocalePreference,
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
