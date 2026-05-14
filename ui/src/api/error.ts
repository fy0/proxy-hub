type ApiErrorInit = {
  status: number;
  statusText: string;
  data: unknown;
  request?: Request;
  response?: Response;
};

function pickMessage(data: unknown, fallback: string): string {
  if (!data) return fallback;

  if (typeof data === 'string') return data;

  if (typeof data === 'object') {
    const candidate = data as Record<string, unknown>;
    if (typeof candidate.message === 'string' && candidate.message.trim() !== '') {
      return candidate.message;
    }

    const details = Array.isArray(candidate.errors)
      ? candidate.errors
          .map(item => {
            if (!item || typeof item !== 'object') return '';
            const detail = item as Record<string, unknown>;
            return typeof detail.message === 'string' ? detail.message.trim() : '';
          })
          .filter(Boolean)
      : [];

    const detail = typeof candidate.detail === 'string' ? candidate.detail.trim() : '';
    if (detail && detail !== 'unexpected error occurred' && detail !== 'validation failed') {
      return detail;
    }
    if (details.length > 0) {
      return details.join('\n');
    }
    if (typeof candidate.error === 'string' && candidate.error.trim() !== '') {
      return candidate.error;
    }
    if (detail) {
      return detail;
    }
    if (typeof candidate.title === 'string' && candidate.title.trim() !== '') {
      return candidate.title;
    }
  }

  return fallback;
}

function pickCode(data: unknown): string {
  if (!data || typeof data !== 'object') return '';

  const candidate = data as Record<string, unknown>;
  return typeof candidate.code === 'string' ? candidate.code : '';
}

export class ApiError extends Error {
  status: number;
  statusText: string;
  code: string;
  data: unknown;
  request?: Request;
  response?: Response;

  constructor(init: ApiErrorInit) {
    const message = pickMessage(init.data, init.statusText);
    super(message);
    this.name = 'ApiError';
    this.status = init.status;
    this.statusText = init.statusText;
    this.code = pickCode(init.data);
    this.data = init.data;
    this.request = init.request;
    this.response = init.response;
  }
}
