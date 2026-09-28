// Загрузка групп правил для выпадающих списков и таблиц.

import { ref } from 'vue'

import { get } from '../api/client'
import type { Group } from '../api/types'

// useGroups возвращает список групп (сначала системные) и функцию его загрузки.
export function useGroups() {
  const groups = ref<Group[]>([])

  async function loadGroups(): Promise<void> {
    const data = await get<{ groups: Group[] }>('/api/groups')

    groups.value = data.groups ?? []
  }

  return { groups, loadGroups }
}

// groupLabel возвращает подпись группы для выпадающего списка.
export function groupLabel(group: Group): string {
  if (group.name === 'block') {
    return 'block — блокировка'
  }

  if (group.name === 'bypass') {
    return 'bypass — мимо туннеля'
  }

  return group.name
}
