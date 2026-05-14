import './assets/main.css';

import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { VueQueryPlugin } from '@tanstack/vue-query';

import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

import App from './App.vue';
import router from './router';

import { setupApiClient } from './api';
import { initI18n } from './i18n';
import { queryClient } from './queryClient';

dayjs.extend(relativeTime);
initI18n();

setupApiClient();

document.documentElement.classList.remove('dark');
document.documentElement.classList.add('light');
document.documentElement.style.colorScheme = 'light';

const app = createApp(App);

app.use(createPinia());
app.use(router);
app.use(VueQueryPlugin, {
  queryClient,
});

app.mount('#app');
