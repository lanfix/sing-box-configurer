// Структура левого меню: одиночные пункты и раскрывающиеся группы.

import { icons } from './icons'

export type NavBadge = 'config' | 'update'

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
  children: NavLink[]
}

export type NavEntry = NavLink | NavGroup

// isNavGroup проверяет, что пункт меню — группа.
export function isNavGroup(entry: NavEntry): entry is NavGroup {
  return 'children' in entry
}

export const navigation: NavEntry[] = [
  { route: 'overview', title: 'Обзор', icon: icons.overview },
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
    ],
  },
  { route: 'outbounds', title: 'Outbounds', icon: icons.outbounds },
  { route: 'inbounds', title: 'Inbounds', icon: icons.inbounds },
  {
    id: 'subscriptions',
    title: 'Подписки',
    icon: icons.subscriptions,
    children: [
      { route: 'happ', title: 'Happ' },
      { route: 'amnezia', title: 'Amnezia' },
    ],
  },
  { route: 'config', title: 'Конфиг', icon: icons.config, badge: 'config' },
  { route: 'control', title: 'Управление', icon: icons.control, badge: 'update' },
]
