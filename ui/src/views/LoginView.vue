<script setup lang="ts">
import { reactive, ref } from 'vue';
import { RouterLink, useRoute, useRouter } from 'vue-router';
import { ArrowLeft, LogIn, ShieldCheck } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { getApiBaseUrl } from '@/api';
import { postUserSignin } from '@/api/generated';
import type { AuthResponse } from '@/api/generated';
import { setAccessToken } from '@/api/auth';
import { useI18n } from '@/i18n';
import './login.css';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const form = reactive({
  username: '',
  password: '',
});
const isSubmitting = ref(false);
const isSkipping = ref(false);
const errorMessage = ref('');
const successMessage = ref('');

function resolveRedirect(): string {
  const redirect = Array.isArray(route.query.redirect)
    ? route.query.redirect[0]
    : route.query.redirect;

  if (typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')) {
    return redirect;
  }

  return '/';
}

function errorMessageFromData(data: unknown, fallback: string): string {
  if (data && typeof data === 'object') {
    const candidate = data as Record<string, unknown>;
    for (const key of ['message', 'detail', 'error']) {
      const value = candidate[key];
      if (typeof value === 'string' && value.trim() !== '') {
        return value;
      }
    }
  }

  return fallback;
}

async function postSkipLogin(): Promise<AuthResponse> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/user/skip-login`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
  });
  const data = (await response.json().catch(() => null)) as unknown;

  if (!response.ok) {
    throw new Error(errorMessageFromData(data, response.statusText));
  }

  return data as AuthResponse;
}

async function handleSubmit(): Promise<void> {
  errorMessage.value = '';
  successMessage.value = '';
  isSubmitting.value = true;

  try {
    const { data } = await postUserSignin({
      body: {
        username: form.username.trim(),
        password: form.password,
      },
      throwOnError: true,
    });

    setAccessToken(data.token);
    successMessage.value = data.message || t('login.messages.success');
    await router.push(resolveRedirect());
  } catch (error) {
    errorMessage.value =
      error instanceof Error && error.message.trim() !== ''
        ? error.message
        : t('login.errors.failed');
  } finally {
    isSubmitting.value = false;
  }
}

async function handleSkipLogin(): Promise<void> {
  errorMessage.value = '';
  successMessage.value = '';
  isSkipping.value = true;

  try {
    const data = await postSkipLogin();

    setAccessToken(data.token);
    successMessage.value = data.message || t('login.messages.skipSuccess');
    await router.push(resolveRedirect());
  } catch (error) {
    errorMessage.value =
      error instanceof Error && error.message.trim() !== ''
        ? error.message
        : t('login.errors.skipFailed');
  } finally {
    isSkipping.value = false;
  }
}
</script>

<template>
  <main class="login-shell">
    <section class="login-panel" aria-labelledby="login-title">
      <RouterLink class="login-back-link" to="/">
        <ArrowLeft class="size-4" aria-hidden="true" />
        {{ t('common.goHome') }}
      </RouterLink>

      <div class="login-heading">
        <span class="login-icon">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <h1 id="login-title">{{ t('login.title') }}</h1>
      </div>

      <form class="login-form" @submit.prevent="handleSubmit">
        <label>
          <span>{{ t('login.form.username') }}</span>
          <input
            v-model.trim="form.username"
            type="text"
            autocomplete="username"
            required
            :placeholder="t('login.placeholders.username')"
          />
        </label>

        <label>
          <span>{{ t('login.form.password') }}</span>
          <input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            required
            :placeholder="t('login.placeholders.password')"
          />
        </label>

        <p v-if="errorMessage" class="login-message error" role="alert">{{ errorMessage }}</p>
        <p v-else-if="successMessage" class="login-message" role="status">{{ successMessage }}</p>

        <Button type="submit" class="login-submit" :disabled="isSubmitting || isSkipping">
          <LogIn class="size-4" aria-hidden="true" />
          {{ t('login.form.submit') }}
        </Button>

        <button
          type="button"
          class="skip-login-button"
          :disabled="isSubmitting || isSkipping"
          @click="handleSkipLogin"
        >
          {{ t('login.form.skip') }}
        </button>
      </form>
    </section>
  </main>
</template>
