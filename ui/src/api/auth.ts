import { ApiError } from './error';

const ACCESS_TOKEN_STORAGE_KEY = 'proxy_hub_access_token';
const LEGACY_ACCESS_TOKEN_STORAGE_KEY = 'token';

function normalizeToken(value: string | null | undefined): string {
  return (value || '').trim().replace(/^Bearer\s+/i, '');
}

function clearAuthorizationCookie(): void {
  if (typeof document === 'undefined') return;

  document.cookie = 'Authorization=; Max-Age=0; path=/; SameSite=Lax';
  document.cookie = 'Authorization=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/';
}

function isUsableToken(token: string): boolean {
  const [tokenId, rawExpiredAt] = token.split(':');
  const expiredAt = Number(rawExpiredAt);

  return (
    tokenId.trim() !== '' &&
    Number.isFinite(expiredAt) &&
    expiredAt > Math.floor(Date.now() / 1000)
  );
}

export function getAccessToken(): string | null {
  try {
    const token = normalizeToken(localStorage.getItem(ACCESS_TOKEN_STORAGE_KEY));
    if (!token) return null;

    if (!isUsableToken(token)) {
      clearAccessToken();
      return null;
    }

    return token;
  } catch {
    return null;
  }
}

export function setAccessToken(token: string): void {
  const normalized = normalizeToken(token);
  if (!normalized) {
    clearAccessToken();
    return;
  }

  try {
    localStorage.setItem(ACCESS_TOKEN_STORAGE_KEY, normalized);
    localStorage.removeItem(LEGACY_ACCESS_TOKEN_STORAGE_KEY);
  } catch {
    // Storage may be unavailable in hardened browser modes.
  }
}

export function clearAccessToken(): void {
  try {
    localStorage.removeItem(ACCESS_TOKEN_STORAGE_KEY);
    localStorage.removeItem(LEGACY_ACCESS_TOKEN_STORAGE_KEY);
  } catch {
    // Storage may be unavailable in hardened browser modes.
  }

  clearAuthorizationCookie();
}

export function isAuthCredentialError(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false;

  return (
    error.code === 'INVALID_TOKEN' ||
    error.code === 'MISSING_TOKEN' ||
    error.message.includes('认证凭证') ||
    error.message.toLowerCase().includes('token')
  );
}
