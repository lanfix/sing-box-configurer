// Структура левого меню: одиночные пункты и раскрывающиеся группы.

import { icons } from './icons'

// NavBadge — источник точки у пункта меню.
export type NavBadge = 'config' | 'update' | 'happ' | 'amnezia' | 'subscriptions'

export interface NavLink {
  route: string
  title: string
  icon?: string
  badge?: NavBadge
}

export interface NavGroup {
  id: string
  title: string
  icon: string
  badge?: NavBadge
  children: NavLink[]
}

export type NavEntry = NavLink | NavGroup

// isNavGroup проверяет, что пункт меню — группа.
export function isNavGroup(entry: NavEntry): entry is NavGroup {
  return 'children' in entry
}

export const navigation: NavEntry[] = [
  { route: 'overview', title: 'Обзор', icon: icons.overview },
  { route: 'proxies', title: 'Прокси', icon: icons.proxies },
  {
    id: 'rules',
    title: 'Правила',
    icon: icons.rules,
    children: [
      { route: 'groups', title: 'Группы' },
      { route: 'rules', title: 'Одиночные' },
      { route: 'url-sources', title: 'Источники URL' },
    ],
  },
  {
    id: 'dns',
    title: 'DNS',
    icon: icons.dns,
    children: [
      { route: 'dns-servers', title: 'Серверы' },
      { route: 'dns-records', title: 'Записи' },
      { route: 'dns-settings', title: 'Настройки' },
      { route: 'dns-advanced', title: 'Расширенные' },
    ],
  },
  {
    id: 'outbounds',
    title: 'Outbounds',
    icon: icons.outbounds,
    children: [
      { route: 'outbounds', title: 'Серверы' },
      { route: 'urltests', title: 'URLTest' },
    ],
  },
  { route: 'inbounds', title: 'Inbounds', icon: icons.inbounds },
  {
    id: 'subscriptions',
    title: 'Подписки',
    icon: icons.subscriptions,
    badge: 'subscriptions',
    children: [
      { route: 'happ', title: 'Happ', badge: 'happ' },
      { route: 'amnezia', title: 'Amnezia', badge: 'amnezia' },
    ],
  },
  { route: 'config', title: 'Конфиг', icon: icons.config, badge: 'config' },
  {
    id: 'system',
    title: 'Система',
    icon: icons.control,
    badge: 'update',
    children: [
      { route: 'system-settings', title: 'Настройки' },
      { route: 'system-update', title: 'Обновление', badge: 'update' },
    ],
  },
]
