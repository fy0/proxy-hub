import { client } from './generated/client.gen';
import { getApiBaseUrl } from './config';
import { ApiError } from './error';

let configured = false;

function getToken(): string | null {
  try {
    return localStorage.getItem('token');
  } catch {
    return null;
  }
}

export function setupApiClient(): void {
  if (configured) return;
  configured = true;

  client.setConfig({
    baseUrl: getApiBaseUrl(),
    credentials: 'include',
  });

  client.interceptors.request.use((request) => {
    const token = getToken();
    if (!token) return request;

    const headers = new Headers(request.headers);
    if (!headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }

    return new Request(request, { headers });
  });

  client.interceptors.error.use((error, response, request) => {
    return new ApiError({
      status: response.status,
      statusText: response.statusText,
      data: error,
      request,
      response,
    });
  });
}
