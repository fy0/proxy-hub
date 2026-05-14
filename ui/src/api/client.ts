import { client } from './generated/client.gen';
import { getApiBaseUrl } from './config';
import { ApiError } from './error';
import { clearAccessToken, getAccessToken, isAuthCredentialError } from './auth';

let configured = false;

export function setupApiClient(): void {
  if (configured) return;
  configured = true;

  client.setConfig({
    baseUrl: getApiBaseUrl(),
    credentials: 'include',
  });

  client.interceptors.request.use((request) => {
    const token = getAccessToken();
    if (!token) return request;

    const headers = new Headers(request.headers);
    if (!headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }

    return new Request(request, { headers });
  });

  client.interceptors.error.use((error, response, request) => {
    const apiError = new ApiError({
      status: response.status,
      statusText: response.statusText,
      data: error,
      request,
      response,
    });

    if (isAuthCredentialError(apiError)) {
      clearAccessToken();
    }

    return apiError;
  });
}
