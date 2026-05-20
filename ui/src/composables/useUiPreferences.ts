import { computed, readonly, ref } from 'vue';

export type ExtraUiInfoPreference = 'default' | 'off' | 'on';

const EXTRA_UI_INFO_PREFERENCE_STORAGE_KEY = 'proxy_hub_extra_ui_info_preference';
const defaultExtraUiInfoPreference: ExtraUiInfoPreference = 'default';

const currentExtraUiInfoPreference = ref<ExtraUiInfoPreference>(
  readStoredExtraUiInfoPreference()
);
const showExtraUiInfo = computed(() => currentExtraUiInfoPreference.value === 'on');

function normalizeExtraUiInfoPreference(
  value: string | null | undefined
): ExtraUiInfoPreference | null {
  const preference = value?.trim();
  if (preference === 'default' || preference === 'off' || preference === 'on') {
    return preference;
  }

  return null;
}

function readStoredExtraUiInfoPreference(): ExtraUiInfoPreference {
  if (typeof localStorage === 'undefined') return defaultExtraUiInfoPreference;

  try {
    const storedPreference = normalizeExtraUiInfoPreference(
      localStorage.getItem(EXTRA_UI_INFO_PREFERENCE_STORAGE_KEY)
    );
    if (storedPreference) return storedPreference;

    localStorage.setItem(EXTRA_UI_INFO_PREFERENCE_STORAGE_KEY, defaultExtraUiInfoPreference);
    return defaultExtraUiInfoPreference;
  } catch {
    return defaultExtraUiInfoPreference;
  }
}

function writeStoredExtraUiInfoPreference(nextPreference: ExtraUiInfoPreference): void {
  if (typeof localStorage === 'undefined') return;

  try {
    localStorage.setItem(EXTRA_UI_INFO_PREFERENCE_STORAGE_KEY, nextPreference);
  } catch {
    // Storage may be unavailable in hardened browser modes.
  }
}

export function setExtraUiInfoPreference(nextPreference: ExtraUiInfoPreference): void {
  const normalizedPreference =
    normalizeExtraUiInfoPreference(nextPreference) ?? defaultExtraUiInfoPreference;
  currentExtraUiInfoPreference.value = normalizedPreference;
  writeStoredExtraUiInfoPreference(normalizedPreference);
}

export function useUiPreferences() {
  return {
    extraUiInfoPreference: readonly(currentExtraUiInfoPreference),
    setExtraUiInfoPreference,
    showExtraUiInfo,
  };
}
