<script setup lang="ts">
import { reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { AmneziaPremium, AmneziaProfile, AWGSupport } from '../../api/types'
import { usePolling } from '../../composables/usePolling'
import { showError, showMessage } from '../../stores/toast'
import { dayLevel, daysLeft, formatAgo, formatDate, pluralDays } from '../../utils/format'

const profiles = ref<AmneziaProfile[]>([])
const support = ref<AWGSupport | null>(null)
const loaded = ref(false)

// busy блокирует фоновое обновление списка во время действий.
const busy = ref(false)
const adding = ref(false)
const actionID = ref('')

const form = reactive({
  key: '',
  name: '',
})

// load загружает профили и поддержку AmneziaWG.
async function load(): Promise<void> {
  if (busy.value) {
    return
  }

  try {
    const data = await get<{ awg_support: AWGSupport; profiles: AmneziaProfile[] }>('/api/amnezia/profiles')

    support.value = data.awg_support
    profiles.value = data.profiles ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки конфигураций')
  } finally {
    loaded.value = true
  }
}

usePolling(load, 30000)

// add импортирует ключ vpn://.
async function add(): Promise<void> {
  busy.value = true
  adding.value = true

  try {
    const result = await post<{ message: string; warning?: string }>('/api/amnezia/profiles/add', { key: form.key.trim(), name: form.name.trim() })

    if (result.warning) {
      showMessage(`${result.message}. Внимание: ${result.warning}`, 'warning')
    } else {
      showMessage(`${result.message}, серверы попадут в итоговый конфиг`)
    }

    form.key = ''
    form.name = ''
  } catch (error) {
    showError(error)
  } finally {
    busy.value = false
    adding.value = false
    await load()
  }
}

// action выполняет обновление или удаление профиля.
async function action(profile: AmneziaProfile, name: 'refresh' | 'delete'): Promise<void> {
  if (name === 'delete' && !confirm('Удалить конфигурацию? Ее серверы пропадут из итогового конфига.\n\nДля Amnezia Premium освободится место устройства в подписке.')) {
    return
  }

  busy.value = true
  actionID.value = profile.id

  try {
    const result = await post<{ message: string; warning?: string }>(`/api/amnezia/profiles/${name}`, { id: profile.id })

    showMessage(result.message, result.warning ? 'warning' : 'success')
  } catch (error) {
    showError(error)
  } finally {
    busy.value = false
    actionID.value = ''
    await load()
  }
}

// setCountry меняет страну сервера подписки.
async function setCountry(profile: AmneziaProfile, country: string): Promise<void> {
  busy.value = true
  actionID.value = profile.id

  try {
    const result = await post('/api/amnezia/profiles/country', { id: profile.id, country })

    showMessage(result.message ?? 'Страна изменена')
  } catch (error) {
    showError(error)
  } finally {
    busy.value = false
    actionID.value = ''
    await load()
  }
}

// status описывает состояние профиля.
function status(profile: AmneziaProfile): { dot: string; text: string } {
  if (profile.last_error) {
    return { dot: 'is-bad', text: 'Ошибка обновления' }
  }

  if (profile.requires_awg && support.value && !support.value.supported && !support.value.error) {
    return { dot: 'is-warn', text: 'Не в конфиге: нужен sing-box с AmneziaWG' }
  }

  return { dot: 'is-good', text: 'Серверы в итоговом конфиге' }
}

// dateTile описывает дату подписки Amnezia Premium ("2026-10-06 12:54:28+00:00").
function dateTile(value?: string): { text: string; foot: string; dot: string } | null {
  if (!value) {
    return null
  }

  const date = new Date(value.replace(' ', 'T'))

  if (isNaN(date.getTime())) {
    return { text: value, foot: '', dot: '' }
  }

  const days = daysLeft(date)

  return {
    text: formatDate(date),
    foot: days < 0 ? 'Истекло' : `Осталось ${days} ${pluralDays(days)}`,
    dot: dayLevel(days),
  }
}

// premiumTiles возвращает плитки дат подписки.
function premiumTiles(premium: AmneziaPremium) {
  return [
    { label: 'Подписка до', date: dateTile(premium.subscription_end), empty: 'Нет данных' },
    { label: 'Конфигурация действует до', date: dateTile(premium.config_expires_at), empty: 'Без срока' },
  ]
}
</script>

<template>
  <div class="add-form-card">
    <div class="form-header">Конфигурации Amnezia</div>
    <p class="card-hint" style="margin-bottom: 16px;">
      Импорт ключа <code>vpn://</code> из приложения Amnezia VPN — формат определяется автоматически: свой сервер
      (AmneziaWG, WireGuard, Xray) или подписка Amnezia Premium (конфигурация запрашивается у шлюза Amnezia).
      Серверы сразу попадают в итоговый конфиг; чтобы они заработали, примените конфиг на странице
      <RouterLink :to="{ name: 'config' }">«Конфиг»</RouterLink>.
    </p>

    <div v-if="!support" class="happ-note">Проверяю версию sing-box...</div>
    <div v-else-if="support.supported" class="happ-note">{{ support.version }} поддерживает AmneziaWG.</div>
    <div v-else class="happ-note is-warn">
      {{ support.version || 'sing-box' }} не поддерживает AmneziaWG<template v-if="support.error"> ({{ support.error }})</template>.
      WireGuard и Xray импортируются как обычно, а серверы AmneziaWG не попадут в конфиг: официальный sing-box
      не запустится с параметрами обфускации. Нужен форк
      <a href="https://github.com/Leadaxe/sing-box-lx" target="_blank" rel="noopener">sing-box-lx</a>.
    </div>

    <form @submit.prevent="add">
      <div class="form-group" style="margin-bottom: 16px;">
        <label class="form-label" for="amneziaKey">Ключ подключения</label>
        <textarea id="amneziaKey" v-model="form.key" class="form-textarea amnezia-key" placeholder="vpn://..." required></textarea>
      </div>
      <div class="form-group" style="margin-bottom: 16px;">
        <label class="form-label" for="amneziaName">Название</label>
        <input id="amneziaName" v-model="form.name" class="form-input" type="text" placeholder="Из ключа">
      </div>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary" :disabled="adding">{{ adding ? 'Импортирую...' : 'Импортировать' }}</button>
      </div>
    </form>
  </div>

  <div class="happ-profiles">
    <div v-if="loaded && profiles.length === 0" class="empty-state">Конфигураций пока нет. Вставьте ключ vpn:// выше.</div>

    <div v-for="profile in profiles" :key="profile.id" class="add-form-card happ-card">
      <div class="happ-card-head">
        <div class="happ-card-title">
          <div class="form-header" style="margin-bottom: 4px;">{{ profile.name }}</div>
          <div class="happ-card-sub">
            <span class="stat-dot" :class="status(profile).dot"></span>
            <span>{{ status(profile).text }}</span>
            <template v-if="profile.server">
              <span class="happ-sep">·</span>
              <span>{{ profile.server }}</span>
            </template>
            <template v-if="profile.premium">
              <span class="happ-sep">·</span>
              <span>обновлено {{ formatAgo(profile.last_update) }}</span>
            </template>
          </div>
        </div>
        <div class="happ-card-actions">
          <button v-if="profile.premium" class="btn btn-secondary btn-sm" :disabled="actionID === profile.id" @click="action(profile, 'refresh')">
            Обновить
          </button>
          <button class="btn btn-danger btn-sm" :disabled="actionID === profile.id" @click="action(profile, 'delete')">Удалить</button>
        </div>
      </div>

      <div v-if="profile.last_error" class="happ-note is-bad">{{ profile.last_error }}</div>

      <div v-if="profile.premium" class="stat-row happ-stats">
        <div v-for="tile in premiumTiles(profile.premium)" :key="tile.label" class="stat-tile">
          <div class="stat-label">{{ tile.label }}</div>
          <div class="stat-value happ-date">{{ tile.date?.text ?? '—' }}</div>
          <div class="stat-foot">
            <template v-if="tile.date">
              <span v-if="tile.date.dot" class="stat-dot" :class="tile.date.dot"></span>{{ tile.date.foot }}
            </template>
            <template v-else>{{ tile.empty }}</template>
          </div>
        </div>
        <div class="stat-tile">
          <div class="stat-label">Страна сервера</div>
          <select
            v-if="profile.premium.available_countries?.length"
            class="form-select amnezia-country"
            :value="profile.premium.server_country_code"
            :disabled="actionID === profile.id"
            @change="setCountry(profile, ($event.target as HTMLSelectElement).value)"
          >
            <option v-for="country in profile.premium.available_countries" :key="country.code" :value="country.code">{{ country.name }}</option>
          </select>
          <div v-else class="stat-value happ-date">{{ profile.premium.server_country_name || '—' }}</div>
          <div class="stat-foot">Смена страны запрашивает новую конфигурацию</div>
        </div>
      </div>

      <div v-if="profile.warnings?.length" class="happ-note">
        <div v-for="warning in profile.warnings" :key="warning">{{ warning }}</div>
      </div>

      <table class="table">
        <thead><tr><th>Протокол</th><th>Тип</th><th>Сервер</th></tr></thead>
        <tbody>
          <tr v-for="item in profile.items" :key="item.tag">
            <td>{{ item.name }}</td>
            <td><span class="badge" :class="item.requires_awg ? 'badge-group' : 'badge-domain'">{{ item.requires_awg ? 'AmneziaWG' : item.protocol }}</span></td>
            <td>{{ item.server }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
