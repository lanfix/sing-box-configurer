<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import { icons } from '../icons'
import { isNavGroup, navigation, type NavBadge, type NavGroup } from '../navigation'
import { configChanged } from '../stores/configStatus'
import { updateAvailable, updates } from '../stores/updates'
import SvgIcon from './SvgIcon.vue'

const route = useRoute()

// storageKey — ключ localStorage с раскрытыми группами меню.
const storageKey = 'navOpenGroups'

const openGroups = ref<string[]>(loadOpenGroups())

// loadOpenGroups читает раскрытые группы из localStorage.
function loadOpenGroups(): string[] {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey) ?? '[]')

    return Array.isArray(value) ? value : []
  } catch {
    return []
  }
}

// saveOpenGroups запоминает раскрытые группы.
function saveOpenGroups(): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify(openGroups.value))
  } catch {
    // localStorage недоступен — состояние меню просто не сохранится.
  }
}

// groupActive проверяет, что открыта страница из группы.
function groupActive(group: NavGroup): boolean {
  return group.children.some((child) => child.route === route.name)
}

// toggleGroup раскрывает или сворачивает группу.
function toggleGroup(group: NavGroup): void {
  if (openGroups.value.includes(group.id)) {
    openGroups.value = openGroups.value.filter((id) => id !== group.id)
  } else {
    openGroups.value = [...openGroups.value, group.id]
  }

  saveOpenGroups()
}

// badgeVisible проверяет, нужно ли показать бейдж у пункта меню.
function badgeVisible(badge?: NavBadge): boolean {
  if (badge === 'config') {
    return configChanged.value
  }

  if (badge === 'update') {
    return updateAvailable.value
  }

  return false
}

// Группа с открытой страницей всегда раскрыта.
function openActiveGroup(): void {
  for (const entry of navigation) {
    if (isNavGroup(entry) && groupActive(entry) && !openGroups.value.includes(entry.id)) {
      openGroups.value = [...openGroups.value, entry.id]
      saveOpenGroups()
    }
  }
}

watch(() => route.name, openActiveGroup)
onMounted(openActiveGroup)
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-header">
      <div class="logo">
        <SvgIcon :path="icons.logo" />
      </div>
      <div class="logo-text">
        <span class="title">Sing-Box</span>
        <span class="subtitle">Configurer {{ updates.check?.current_version ?? '' }}</span>
      </div>
    </div>

    <nav class="nav-menu">
      <template v-for="entry in navigation" :key="isNavGroup(entry) ? entry.id : entry.route">
        <div
          v-if="isNavGroup(entry)"
          class="nav-group"
          :class="{ open: openGroups.includes(entry.id), 'has-active': groupActive(entry) }"
        >
          <div class="nav-item nav-group-toggle" @click="toggleGroup(entry)">
            <div class="nav-icon"><SvgIcon :path="entry.icon" /></div>
            <span>{{ entry.title }}</span>
            <SvgIcon class="nav-chevron" :path="icons.chevron" />
          </div>

          <div v-show="openGroups.includes(entry.id)" class="nav-children">
            <RouterLink
              v-for="child in entry.children"
              :key="child.route"
              :to="{ name: child.route }"
              class="nav-item"
              active-class="active"
            >
              <span>{{ child.title }}</span>
            </RouterLink>
          </div>
        </div>

        <RouterLink v-else :to="{ name: entry.route }" class="nav-item" exact-active-class="active">
          <div class="nav-icon"><SvgIcon :path="entry.icon ?? ''" /></div>
          <span>{{ entry.title }}</span>
          <span
            v-if="badgeVisible(entry.badge)"
            class="badge-pending"
            :title="entry.badge === 'config' ? 'Итоговый конфиг отличается от рабочего' : 'Доступно обновление'"
          ></span>
        </RouterLink>
      </template>
    </nav>
  </aside>
</template>
