// Данные, которые карта передает в компоненты узлов.

import type { FlowStats, MapNode } from './model'

// Selection — что выбрано на карте: узел, связь, строка маршрутизатора или результат трассировки.
export type Selection =
  | { type: 'node'; id: string }
  | { type: 'edge'; id: string }
  | { type: 'row'; node: string; row: string }
  | { type: 'trace' }

// NodeState — подсветка узла: на пути трассировки, приглушен или обычный.
export type NodeState = '' | 'path' | 'dim'

export interface NodeViewData {
  item: MapNode
  state: NodeState
  live?: FlowStats

  // Для маршрутизаторов: живой трафик по строкам, строки на пути трассировки и выбранная строка.
  rows?: Record<string, FlowStats>
  pathRows?: string[]
  selectedRow?: string
  onRow?: (nodeId: string, rowId: string) => void
}

// kindLabels — подписи видов узлов.
export const kindLabels: Record<string, string> = {
  inbound: 'Inbound',
  router: 'Маршрутизация',
  'dns-router': 'DNS',
  selector: 'Группа',
  urltest: 'URLTest',
  outbound: 'Outbound',
  action: 'Действие',
  'dns-server': 'DNS-сервер',
  cluster: 'Подписка',
}

// plural возвращает число с подходящей формой слова: один, два, пять.
export function plural(count: number, one: string, few: string, many: string): string {
  const mod10 = count % 10
  const mod100 = count % 100

  if (mod10 === 1 && mod100 !== 11) {
    return `${count} ${one}`
  }

  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return `${count} ${few}`
  }

  return `${count} ${many}`
}

// actionLabels — подписи действий правил.
export const actionLabels: Record<string, string> = {
  route: 'маршрут',
  reject: 'отклонить',
  bypass: 'мимо',
  sniff: 'sniff',
  resolve: 'резолв',
  'hijack-dns': 'в DNS',
  'route-options': 'опции',
  predefined: 'ответ',
}
