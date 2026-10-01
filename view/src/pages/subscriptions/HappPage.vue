<script setup lang="ts">
import { reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { HappProfile } from '../../api/types'
import SvgIcon from '../../components/SvgIcon.vue'
import FormField from '../../components/ui/FormField.vue'
import { usePolling } from '../../composables/usePolling'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { refreshSubscriptionAlerts } from '../../stores/subscriptionAlerts'
import { showError, showMessage } from '../../stores/toast'
import { dayLevel, daysLeft, formatAgo, formatBytesRu, formatDate, pluralDays, safeURL } from '../../utils/format'

const profiles = ref<HappProfile[]>([])
const installationID = ref('—')
const loaded = ref(false)

// busy блокирует фоновое обновление списка во время действий.
const busy = ref(false)
const adding = ref(false)
const actionID = ref('')

const form = reactive({
  url: '',
  name: '',
})

// load загружает профили.
async function load(): Promise<void> {
  if (busy.value) {
    return
  }

  try {
    const data = await get<{ installation_id: string; profiles: HappProfile[] }>('/api/happ/profiles')

    installationID.value = data.installation_id || '—'
    profiles.value = data.profiles ?? []

    void refreshSubscriptionAlerts()
  } catch (error) {
    showError(error, 'Ошибка загрузки подписок')
  } finally {
    loaded.value = true
  }
}

usePolling(load, 30000)

// add добавляет подписку.
async function add(): Promise<void> {
  busy.value = true
  adding.value = true

  try {
    await post('/api/happ/profiles/add', { url: form.url.trim(), name: form.name.trim() })
    showMessage('Подписка добавлена, ее серверы попадут в итоговый конфиг')
    form.url = ''
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
async function action(profile: HappProfile, name: 'refresh' | 'delete'): Promise<void> {
  const confirmed = name !== 'delete' || await confirmAction({
    title: `Удалить подписку ${profile.name}?`,
    message: 'Ее серверы пропадут из итогового конфига. Место устройства в подписке освободите в боте провайдера.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  busy.value = true
  actionID.value = profile.id

  try {
    const result = await post(`/api/happ/profiles/${name}`, { id: profile.id })

    showMessage(result.message ?? 'Готово')
  } catch (error) {
    showError(error)
  } finally {
    busy.value = false
    actionID.value = ''
    await load()
  }
}

// copyHWID копирует ID инсталляции.
async function copyHWID(): Promise<void> {
  try {
    await navigator.clipboard.writeText(installationID.value)
    showMessage('HWID скопирован')
  } catch {
    showMessage(`Не удалось скопировать: ${installationID.value}`, 'error')
  }
}

// traffic описывает расход трафика подписки.
function traffic(profile: HappProfile): { value: string; total: string; foot: string; percent: number; level: string } {
  const info = profile.info ?? {}
  const used = (info.upload ?? 0) + (info.download ?? 0)
  const total = info.total ?? 0

  if (total <= 0) {
    return { value: formatBytesRu(used), total: '', foot: 'Без лимита трафика', percent: 0, level: '' }
  }

  const percent = Math.min(100, used / total * 100)

  return {
    value: formatBytesRu(used),
    total: `из ${formatBytesRu(total)}`,
    foot: `Осталось ${formatBytesRu(Math.max(0, total - used))}`,
    percent,
    level: percent >= 90 ? 'is-bad' : percent >= 75 ? 'is-warn' : 'is-good',
  }
}

// expire описывает срок действия подписки.
function expire(profile: HappProfile): { value: string; foot: string; dot: string } {
  if (!profile.info?.expire) {
    return { value: '∞', foot: 'Бессрочно', dot: 'is-good' }
  }

  const date = new Date(profile.info.expire)
  const days = daysLeft(date)

  if (days < 0) {
    return { value: formatDate(date), foot: 'Подписка истекла', dot: 'is-bad' }
  }

  return { value: formatDate(date), foot: `Осталось ${days} ${pluralDays(days)}`, dot: dayLevel(days) }
}

// proxyCount — количество серверов без urltest-групп.
function proxyCount(profile: HappProfile): number {
  return profile.servers.filter((server) => server.type === 'proxy').length
}
</script>

<template>
  <p class="page-intro">
    Конфигуратор запрашивает подписку как приложение Happ на iPhone и превращает ее серверы в outbound-ы sing-box.
    Серверы сразу попадают в итоговый конфиг; чтобы они заработали, примените
    <RouterLink :to="{ name: 'config' }">конфиг</RouterLink>.
  </p>

  <div class="add-form-card">
    <div class="form-header">Добавить подписку</div>
    <form class="form-stack" @submit.prevent="add">
      <div class="form-grid happ-add-grid">
        <FormField label="Ссылка на подписку" input-id="happURL" hint="Ссылка из бота или личного кабинета провайдера.">
          <input id="happURL" v-model="form.url" class="form-input" type="url" placeholder="https://sub.example.com/..." required spellcheck="false">
        </FormField>
        <FormField label="Название" input-id="happName" optional hint="По умолчанию — из подписки.">
          <input id="happName" v-model="form.name" class="form-input" type="text" placeholder="Мой VPN">
        </FormField>
      </div>

      <FormField label="HWID устройства" input-id="happHWID">
        <div class="input-group">
          <input id="happHWID" class="form-input is-mono" type="text" :value="installationID" readonly>
          <button type="button" class="btn btn-secondary" @click="copyHWID">
            <SvgIcon class="btn-icon" :path="icons.copy" />
            Копировать
          </button>
        </div>
        <template #hint>
          Под этим ID конфигуратор занимает одно место устройства в подписке. Он создается один раз и хранится в
          <code>app.json</code>.
        </template>
      </FormField>

      <div class="form-actions" style="margin-top: 0;">
        <button type="submit" class="btn btn-primary" :disabled="adding">{{ adding ? 'Загружаю подписку...' : 'Добавить подписку' }}</button>
      </div>
    </form>
  </div>

  <div class="happ-profiles">
    <div v-if="loaded && profiles.length === 0" class="empty-state">Подписок пока нет. Добавьте ссылку выше.</div>

    <div v-for="profile in profiles" :key="profile.id" class="add-form-card happ-card">
      <div class="happ-card-head">
        <div class="happ-card-title">
          <div class="form-header" style="margin-bottom: 4px;">{{ profile.name }}</div>
          <div class="happ-card-sub">
            <span class="stat-dot" :class="profile.last_error ? 'is-bad' : 'is-good'"></span>
            <span>{{ profile.last_error ? 'Ошибка обновления' : 'Серверы в итоговом конфиге' }}</span>
            <span class="happ-sep">·</span>
            <span :title="profile.url">обновлено {{ formatAgo(profile.last_update) }}</span>
            <template v-if="safeURL(profile.info?.support_url)">
              <span class="happ-sep">·</span>
              <a :href="safeURL(profile.info?.support_url)" target="_blank" rel="noopener noreferrer">Поддержка</a>
            </template>
            <template v-if="safeURL(profile.info?.web_page_url)">
              <span class="happ-sep">·</span>
              <a :href="safeURL(profile.info?.web_page_url)" target="_blank" rel="noopener noreferrer">Кабинет</a>
            </template>
          </div>
        </div>
        <div class="happ-card-actions">
          <button class="btn btn-secondary btn-sm" :disabled="actionID === profile.id" @click="action(profile, 'refresh')">
            {{ actionID === profile.id ? '...' : 'Обновить' }}
          </button>
          <button class="btn btn-danger btn-sm" :disabled="actionID === profile.id" @click="action(profile, 'delete')">Удалить</button>
        </div>
      </div>

      <div v-if="profile.last_error" class="happ-note is-bad">{{ profile.last_error }}</div>
      <div v-if="profile.info?.announce" class="happ-note">{{ profile.info.announce }}</div>

      <div class="stat-row happ-stats">
        <div class="stat-tile">
          <div class="stat-label">Потрачено трафика</div>
          <div class="stat-value">
            {{ traffic(profile).value }}
            <span v-if="traffic(profile).total" class="stat-unit">{{ traffic(profile).total }}</span>
          </div>
          <div v-if="traffic(profile).total" class="happ-meter">
            <div class="happ-meter-fill" :class="traffic(profile).level" :style="{ width: `${traffic(profile).percent.toFixed(1)}%` }"></div>
          </div>
          <div class="stat-foot">{{ traffic(profile).foot }}</div>
        </div>
        <div class="stat-tile">
          <div class="stat-label">Действует до</div>
          <div class="stat-value happ-date">{{ expire(profile).value }}</div>
          <div class="stat-foot"><span class="stat-dot" :class="expire(profile).dot"></span>{{ expire(profile).foot }}</div>
        </div>
        <div class="stat-tile">
          <div class="stat-label">Серверы</div>
          <div class="stat-value">
            {{ proxyCount(profile) }}
            <span class="stat-unit">{{ profile.servers.length > proxyCount(profile) ? `+ ${profile.servers.length - proxyCount(profile)} авто` : 'шт' }}</span>
          </div>
          <div class="stat-foot">{{ profile.warnings?.length ? `Пропущено: ${profile.warnings.length}` : 'Все поддерживаются sing-box' }}</div>
        </div>
      </div>

      <div v-if="profile.warnings?.length" class="happ-note">Не поддерживаются sing-box: {{ profile.warnings.join('; ') }}</div>

      <details class="happ-servers">
        <summary>Список серверов</summary>
        <table class="table">
          <thead><tr><th>Название</th><th>Тип</th><th>Адрес</th></tr></thead>
          <tbody>
            <tr v-for="server in profile.servers" :key="server.tag">
              <td>{{ server.name }}</td>
              <td><span class="badge" :class="server.type === 'urltest' ? 'badge-group' : 'badge-domain'">{{ server.protocol }}</span></td>
              <td>
                <template v-if="server.address">{{ server.address }}:{{ server.port }}</template>
                <span v-else class="card-hint">самый быстрый из {{ ((server.outbound.outbounds as unknown[]) ?? []).length }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </details>
    </div>
  </div>
</template>
