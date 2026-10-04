<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { get, post } from '../api/client'
import type { MixedInbound } from '../api/types'
import ModalDialog from '../components/ModalDialog.vue'
import SvgIcon from '../components/SvgIcon.vue'
import FormField from '../components/ui/FormField.vue'
import HelpHint from '../components/ui/HelpHint.vue'
import IconButton from '../components/ui/IconButton.vue'
import { icons } from '../icons'
import { confirmAction } from '../stores/confirm'
import { showError, showMessage } from '../stores/toast'
import { tagError } from '../utils/validate'

// listenPresets — частые адреса прослушивания.
const listenPresets = [
  { value: '0.0.0.0', label: 'Вся сеть' },
  { value: '127.0.0.1', label: 'Только этот хост' },
]

const mixed = ref<MixedInbound[]>([])
const loaded = ref(false)

// Редактор mixed-inbound-а: editing — тег редактируемого или пустая строка для нового.
const editor = ref<{ editing: string; value: MixedInbound; error: string; saving: boolean } | null>(null)

// visiblePasswords — индексы пользователей, у которых пароль показан.
const visiblePasswords = ref<number[]>([])

const tagProblem = computed(() => tagError(editor.value?.value.tag ?? ''))

// load загружает mixed-inbound-ы.
async function load(): Promise<void> {
  try {
    const data = await get<{ mixed: MixedInbound[] }>('/api/inbounds')

    mixed.value = data.mixed ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки inbound-ов')
  } finally {
    loaded.value = true
  }
}

// openEditor открывает форму нового или существующего inbound-а.
function openEditor(inbound?: MixedInbound): void {
  visiblePasswords.value = []
  editor.value = {
    editing: inbound?.tag ?? '',
    value: inbound
      ? { ...inbound, users: inbound.users.map((user) => ({ ...user })) }
      : { tag: 'mixed-proxy', listen: '0.0.0.0', listen_port: 1080, users: [], extra: null },
    error: '',
    saving: false,
  }
}

// generatePassword возвращает случайный пароль.
function generatePassword(): string {
  const alphabet = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  const bytes = crypto.getRandomValues(new Uint8Array(16))

  return Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join('')
}

// addUser добавляет пользователя со сгенерированным паролем.
function addUser(): void {
  if (!editor.value) {
    return
  }

  editor.value.value.users.push({ username: '', password: generatePassword() })
  visiblePasswords.value = [...visiblePasswords.value, editor.value.value.users.length - 1]
}

// removeUser удаляет пользователя из формы.
function removeUser(index: number): void {
  editor.value?.value.users.splice(index, 1)
  visiblePasswords.value = []
}

// togglePassword показывает или скрывает пароль пользователя.
function togglePassword(index: number): void {
  visiblePasswords.value = visiblePasswords.value.includes(index)
    ? visiblePasswords.value.filter((item) => item !== index)
    : [...visiblePasswords.value, index]
}

// save сохраняет inbound. Ошибка показывается прямо в форме.
async function save(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  current.saving = true
  current.error = ''

  try {
    await post(current.editing ? '/api/inbounds/mixed/edit' : '/api/inbounds/mixed/add', current.value)
    showMessage(`Inbound ${current.value.tag} сохранен. Он заработает после применения конфига.`)
    editor.value = null
    await load()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// remove удаляет inbound.
async function remove(inbound: MixedInbound): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить inbound ${inbound.tag}?`,
    message: `Прокси на порту ${inbound.listen_port} перестанет работать после применения конфига.`,
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/inbounds/mixed/delete', { tag: inbound.tag })
    showMessage('Inbound удален')
    await load()
  } catch (error) {
    showError(error)
  }
}

onMounted(load)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-primary" @click="openEditor()">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить прокси
    </button>
  </Teleport>

  <p class="page-intro">
    Входящие подключения sing-box: как трафик попадает в него. Основной путь — прозрачный туннель, дополнительно
    можно открыть HTTP/SOCKS5-прокси для программ, которые умеют работать через прокси.
  </p>

  <div class="section-head">
    <h3 class="section-title">Mixed-прокси (HTTP и SOCKS5)</h3>
  </div>

  <HelpHint>
    <p>
      HTTP и SOCKS5 работают на одном порту. Домены, полученные через прокси, резолвятся до маршрутизации (правило
      <code>resolve</code>), поэтому для них действуют и IP-правила групп.
    </p>
    <p>Без пользователей прокси открыт всем, кто может подключиться к порту.</p>
  </HelpHint>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Адрес</th>
          <th>Доступ</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && mixed.length === 0">
          <td colspan="4" class="empty-state">Прокси нет. Добавьте, если нужен HTTP/SOCKS5-доступ.</td>
        </tr>
        <tr v-for="inbound in mixed" :key="inbound.tag">
          <td><span class="cell-main">{{ inbound.tag }}</span></td>
          <td data-label="Адрес"><span class="cell-mono">{{ inbound.listen }}:{{ inbound.listen_port }}</span></td>
          <td data-label="Доступ">
            <span v-if="inbound.users.length">
              По паролю: {{ inbound.users.map((user) => user.username).join(', ') }}
            </span>
            <span v-else class="status-badge status-pending" title="Подключиться может любой, кто видит порт">без пароля</span>
          </td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" @click="openEditor(inbound)" />
              <IconButton icon="trash" title="Удалить" danger @click="remove(inbound)" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="section-head">
    <h3 class="section-title">Встроенные</h3>
  </div>
  <p class="section-hint">Всегда есть в итоговом конфиге, не настраиваются.</p>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Назначение</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td>
            <span class="cell-main">tun-in</span>
            <div class="cell-sub">tun</div>
          </td>
          <td class="description-cell">
            Прозрачный туннель tun0 (198.18.0.1/30) с auto_route и auto_redirect: перехватывает трафик устройств сети.
            Адреса группы bypass исключаются в nftables (<code>route_exclude_address_set</code>).
          </td>
        </tr>
        <tr>
          <td>
            <span class="cell-main">dns-in</span>
            <div class="cell-sub">direct</div>
          </td>
          <td class="description-cell">DNS-сервер для устройств сети на 0.0.0.0:53: запросы обрабатываются DNS sing-box (hijack-dns).</td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="editor" :title="editor.editing ? `Прокси ${editor.editing}` : 'Новый mixed-прокси'" @close="editor = null">
    <form class="form-stack" @submit.prevent="save">
      <FormField
        label="Тег"
        input-id="inboundTag"
        :error="tagProblem"
        :hint="editor.editing ? 'Тег не меняется после создания.' : 'Короткое имя латиницей.'"
      >
        <input
          id="inboundTag"
          v-model="editor.value.tag"
          class="form-input"
          type="text"
          required
          autocomplete="off"
          spellcheck="false"
          :disabled="Boolean(editor.editing)"
        >
      </FormField>

      <div class="form-grid inbound-address-grid">
        <FormField label="Слушать адрес" input-id="inboundListen">
          <input id="inboundListen" v-model="editor.value.listen" class="form-input" type="text" placeholder="0.0.0.0" spellcheck="false">
          <template #hint>
            <span class="preset-list">
              <button
                v-for="preset in listenPresets"
                :key="preset.value"
                type="button"
                class="preset-chip"
                :class="{ active: editor.value.listen === preset.value }"
                @click="editor.value.listen = preset.value"
              >
                {{ preset.label }} · {{ preset.value }}
              </button>
            </span>
          </template>
        </FormField>
        <FormField label="Порт" input-id="inboundPort">
          <input id="inboundPort" v-model.number="editor.value.listen_port" class="form-input" type="number" min="1" max="65535" required>
        </FormField>
      </div>

      <div class="field">
        <span class="field-label">Пользователи <span class="field-optional">необязательно</span></span>
        <div class="list-editor">
          <div v-for="(user, index) in editor.value.users" :key="index" class="list-editor-row">
            <input v-model="user.username" class="form-input" type="text" placeholder="Имя" required autocomplete="off" aria-label="Имя пользователя">
            <div class="input-group" style="flex: 1;">
              <input
                v-model="user.password"
                class="form-input is-mono"
                :type="visiblePasswords.includes(index) ? 'text' : 'password'"
                placeholder="Пароль"
                required
                autocomplete="new-password"
                aria-label="Пароль"
              >
              <span class="input-addon secret-actions">
                <IconButton :icon="visiblePasswords.includes(index) ? 'eyeOff' : 'eye'" :title="visiblePasswords.includes(index) ? 'Скрыть' : 'Показать'" @click="togglePassword(index)" />
                <IconButton icon="refresh" title="Сгенерировать пароль" @click="user.password = generatePassword()" />
              </span>
            </div>
            <IconButton icon="trash" title="Удалить пользователя" danger @click="removeUser(index)" />
          </div>
          <div>
            <button type="button" class="btn btn-secondary btn-sm" @click="addUser">
              <SvgIcon class="btn-icon" :path="icons.plus" />
              Добавить пользователя
            </button>
          </div>
        </div>
        <p v-if="editor.value.users.length === 0" class="field-warning">
          Без пользователей прокси открыт всем, кто может подключиться к порту.
        </p>
      </div>

      <div v-if="editor.error" class="form-error">{{ editor.error }}</div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving || Boolean(tagProblem)">
          {{ editor.saving ? 'Сохранение...' : 'Сохранить' }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
