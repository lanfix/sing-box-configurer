// Маршруты интерфейса. Каждая страница — отдельный чанк, который загружается при переходе на нее.

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    title: string
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'overview',
    component: () => import('./pages/OverviewPage.vue'),
    meta: { title: 'Обзор' },
  },
  {
    path: '/proxies',
    name: 'proxies',
    component: () => import('./pages/ProxiesPage.vue'),
    meta: { title: 'Прокси-группы' },
  },
  {
    path: '/rules/groups',
    name: 'groups',
    component: () => import('./pages/rules/GroupsPage.vue'),
    meta: { title: 'Группы' },
  },
  {
    path: '/rules/single',
    name: 'rules',
    component: () => import('./pages/rules/RulesPage.vue'),
    meta: { title: 'Одиночные правила' },
  },
  {
    path: '/rules/url-sources',
    name: 'url-sources',
    component: () => import('./pages/rules/UrlSourcesPage.vue'),
    meta: { title: 'Источники URL' },
  },
  {
    path: '/dns/servers',
    name: 'dns-servers',
    component: () => import('./pages/dns/DnsServersPage.vue'),
    meta: { title: 'DNS-серверы' },
  },
  {
    path: '/dns/records',
    name: 'dns-records',
    component: () => import('./pages/dns/DnsRecordsPage.vue'),
    meta: { title: 'DNS-записи' },
  },
  {
    path: '/dns/advanced',
    name: 'dns-advanced',
    component: () => import('./pages/dns/DnsAdvancedPage.vue'),
    meta: { title: 'Расширенные настройки DNS' },
  },
  {
    path: '/dns/settings',
    name: 'dns-settings',
    component: () => import('./pages/dns/DnsSettingsPage.vue'),
    meta: { title: 'Настройки DNS' },
  },
  {
    path: '/outbounds',
    name: 'outbounds',
    component: () => import('./pages/outbounds/ServersPage.vue'),
    meta: { title: 'Outbound-серверы' },
  },
  {
    path: '/outbounds/urltest',
    name: 'urltests',
    component: () => import('./pages/outbounds/URLTestsPage.vue'),
    meta: { title: 'URLTest' },
  },
  {
    path: '/inbounds',
    name: 'inbounds',
    component: () => import('./pages/InboundsPage.vue'),
    meta: { title: 'Inbounds' },
  },
  {
    path: '/subscriptions/happ',
    name: 'happ',
    component: () => import('./pages/subscriptions/HappPage.vue'),
    meta: { title: 'Подписки Happ' },
  },
  {
    path: '/subscriptions/amnezia',
    name: 'amnezia',
    component: () => import('./pages/subscriptions/AmneziaPage.vue'),
    meta: { title: 'Конфигурации Amnezia' },
  },
  {
    path: '/config',
    name: 'config',
    component: () => import('./pages/ConfigPage.vue'),
    meta: { title: 'Конфигурация sing-box' },
  },
  {
    path: '/system/settings',
    name: 'system-settings',
    component: () => import('./pages/system/SettingsPage.vue'),
    meta: { title: 'Настройки' },
  },
  {
    path: '/system/update',
    name: 'system-update',
    component: () => import('./pages/system/UpdatePage.vue'),
    meta: { title: 'Обновление' },
  },
  // Старый адрес страницы «Управление».
  {
    path: '/control',
    redirect: '/system/settings',
    meta: { title: '' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
    meta: { title: '' },
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} — Sing-Box Конфигуратор` : 'Sing-Box Конфигуратор'
})
