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
    if (typeof candidate.detail === 'string' && candidate.detail.trim() !== '') {
      return candidate.detail;
    }
    if (typeof candidate.error === 'string' && candidate.error.trim() !== '') {
      return candidate.error;
    }
  }

  return fallback;
}

export class ApiError extends Error {
  status: number;
  statusText: string;
  data: unknown;
  request?: Request;
  response?: Response;

  constructor(init: ApiErrorInit) {
    const message = pickMessage(init.data, init.statusText);
    super(message);
    this.name = 'ApiError';
    this.status = init.status;
    this.statusText = init.statusText;
    this.data = init.data;
    this.request = init.request;
    this.response = init.response;
  }
}

