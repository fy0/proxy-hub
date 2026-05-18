import { createRouter, createWebHistory } from 'vue-router';
import HomeView from '../views/HomeView.vue';

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView,
      props: { tab: 'mappings' },
    },
    {
      path: '/nodes',
      name: 'nodes',
      component: HomeView,
      props: { tab: 'nodes' },
    },
    {
      path: '/groups',
      name: 'groups',
      component: HomeView,
      props: { tab: 'groups' },
    },
    {
      path: '/subscriptions',
      name: 'subscriptions',
      component: HomeView,
      props: { tab: 'subscriptions' },
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
    },
    {
      path: '/about',
      name: 'about',
      // route level code-splitting
      // this generates a separate chunk (About.[hash].js) for this route
      // which is lazy-loaded when the route is visited.
      component: () => import('../views/AboutView.vue'),
    },
  ],
});

export default router;
