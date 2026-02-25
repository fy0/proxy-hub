function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '');
}

/**
 * API Base URL.
 *
 * - If `VITE_API_BASE_URL` is set, use it.
 * - Otherwise, use same-origin (`''`).
 */
export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_API_BASE_URL;
  if (typeof fromEnv === 'string' && fromEnv.trim() !== '') {
    return trimTrailingSlash(fromEnv.trim());
  }

  return '';
}
