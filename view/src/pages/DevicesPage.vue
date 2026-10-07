<script setup lang="ts">
import { computed, reactive, ref } from 'vue'

import { get, post, postQuiet } from '../api/client'
import type { Device, DeviceProfile, DeviceSettings, DevicesState, DNSData, DNSServer } from '../api/types'
import ModalDialog from '../components/ModalDialog.vue'
import SvgIcon from '../components/SvgIcon.vue'
import ChipsInput from '../components/ui/ChipsInput.vue'
import FormField from '../components/ui/FormField.vue'
import HelpHint from '../components/ui/HelpHint.vue'
import IconButton from '../components/ui/IconButton.vue'
import SaveBar from '../components/ui/SaveBar.vue'
import SegmentedControl from '../components/ui/SegmentedControl.vue'
import SettingRow from '../components/ui/SettingRow.vue'
import ToggleSwitch from '../components/ui/ToggleSwitch.vue'
import { usePolling } from '../composables/usePolling'
import { useLeaveGuard, useSavedState } from '../composables/useSavedState'
import { icons } from '../icons'
import { confirmAction } from '../stores/confirm'
import { refreshDeviceAlerts } from '../stores/deviceAlerts'
import { showError, showMessage } from '../stores/toast'
import { formatAgo } from '../utils/format'

// profileLabels — подписи профилей.
const profileLabels: Record<Exclude<DeviceProfile, 'default'>, string> = {
  proxy: 'Обход',
  direct: 'Без обхода',
  blocked: 'Без интернета',
}

// profileHints — что делает профиль.
const profileHints: Record<Exclude<DeviceProfile, 'default'>, string> = {
  proxy: 'Трафик идет по группам правил, заблокированные сайты открываются через VPN',
  direct: 'Весь трафик и DNS напрямую, как у провайдера: обход не виден',
  blocked: 'Соединения и DNS-запросы отклоняются',
}

const state = ref<DevicesState | null>(null)
const dnsServers = ref<DNSServer[]>([])
const scanning = ref(false)
const saving = ref(false)

const form = reactive<DeviceSettings>({
  default_profile: 'proxy',
  auto_networks: true,
  networks: [],
  exclude: [],
  direct_dns_server: '',
})

const { dirty, markSaved, reset } = useSavedState(form)

useLeaveGuard(() => dirty.value)

// Редактор устройства: editing — устройство уже добавлено (MAC не меняется).
const editor = ref<{ editing: boolean; mac: string; name: string; profile: DeviceProfile; error: string; saving: boolean } | null>(null)

const defaultLabel = computed(() => profileLabels[state.value?.settings.default_profile ?? 'proxy'])

const profileOptions = computed(() => [
  { value: 'default' as DeviceProfile, label: `По умолчанию (${defaultLabel.value})`, title: 'Профиль для неизвестных устройств' },
  ...(['proxy', 'direct', 'blocked'] as const).map((value) => ({ value: value as DeviceProfile, label: profileLabels[value], title: profileHints[value] })),
])

const defaultOptions = (['proxy', 'direct', 'blocked'] as const).map((value) => ({ value, label: profileLabels[value], title: profileHints[value] }))

// unsupported — запущенный sing-box не понимает MAC-адреса в rule-set-ах.
const unsupported = computed(() => Boolean(state.value?.support.known && !state.value.support.supported))

// noNetworks — профиль по умолчанию ограничивает доступ, но сети LAN не найдены и он не действует.
const noNetworks = computed(() => form.default_profile !== 'proxy' && state.value !== null && state.value.networks.length === 0)

// load загружает устройства. Форма политики заполняется только при первой загрузке и после сохранения.
async function load(fillForm = false): Promise<void> {
  try {
    state.value = await get<DevicesState>('/api/devices')

    if (fillForm || !dirty.value) {
      fillSettings(state.value.settings)
    }
  } catch (error) {
    showError(error, 'Ошибка загрузки устройств')
  }
}

// fillSettings переносит политику в форму.
function fillSettings(settings: DeviceSettings): void {
  form.default_profile = settings.default_profile
  form.auto_networks = settings.auto_networks
  form.networks = [...(settings.networks ?? [])]
  form.exclude = [...(settings.exclude ?? [])]
  form.direct_dns_server = settings.direct_dns_server ?? ''
  markSaved()
}

// loadDNS загружает DNS-серверы для выбора сервера устройств без обхода.
async function loadDNS(): Promise<void> {
  try {
    dnsServers.value = (await get<DNSData>('/api/dns')).servers
  } catch (error) {
    showError(error, 'Ошибка загрузки DNS-серверов')
  }
}

// scan перечитывает таблицу соседей хоста.
async function scan(): Promise<void> {
  scanning.value = true

  try {
    state.value = await postQuiet<DevicesState>('/api/devices/scan')
    void refreshDeviceAlerts()

    // Имена новых устройств запрашиваются в фоне за несколько секунд.
    setTimeout(() => void load(), 5000)
  } catch (error) {
    showError(error)
  } finally {
    scanning.value = false
  }
}

// nameSources — откуда известно имя устройства.
const nameSources: Record<NonNullable<Device['name_source']>, string> = {
  router: 'имя от роутера (DHCP)',
  mdns: 'имя от самого устройства (mDNS)',
  netbios: 'имя компьютера (NetBIOS)',
}

// deviceTitle возвращает подпись устройства: заданное имя, найденное в сети, производителя или MAC.
function deviceTitle(device: Device): string {
  return device.name || device.hostname || device.vendor || device.mac
}

// deviceDetails возвращает вторую строку: MAC, найденное имя, производителя и интерфейс, если их нет в подписи.
function deviceDetails(device: Device): string {
  const title = deviceTitle(device)
  const parts: string[] = []

  if (device.mac !== title) {
    parts.push(device.mac)
  }

  if (device.hostname && device.hostname !== title) {
    parts.push(device.hostname)
  }

  if (device.vendor && device.vendor !== title) {
    parts.push(device.vendor)
  }

  if (device.interface) {
    parts.push(device.interface)
  }

  return parts.join(' · ')
}

// deviceHint возвращает подсказку к подписи: откуда имя и что за случайный MAC.
function deviceHint(device: Device): string {
  const hints: string[] = []

  if (!device.name && device.hostname && device.name_source) {
    hints.push(`Найдено в сети: ${nameSources[device.name_source]}`)
  }

  if (device.random_mac) {
    hints.push('Случайный MAC: телефон или ноутбук скрывает заводской адрес в этой сети Wi-Fi, производитель по нему не определяется')
  }

  return hints.join('. ')
}

// openEditor открывает форму устройства: добавленного или найденного в сети. Новому устройству
// подставляется имя, найденное в сети.
function openEditor(device: Device, added: boolean): void {
  editor.value = {
    editing: added,
    mac: device.mac,
    name: added ? device.name : device.hostname ?? '',
    profile: device.profile,
    error: '',
    saving: false,
  }
}

// openNew открывает форму устройства, которое добавляется по MAC вручную.
function openNew(): void {
  editor.value = { editing: false, mac: '', name: '', profile: 'proxy', error: '', saving: false }
}

// saveDevice сохраняет устройство. Ошибка показывается в форме.
async function saveDevice(): Promise<void> {
  const current = editor.value

  if (!current) {
    return
  }

  current.saving = true
  current.error = ''

  try {
    await post(current.editing ? '/api/devices/edit' : '/api/devices/add', {
      mac: current.mac,
      name: current.name,
      profile: current.profile,
    })

    showMessage('Устройство сохранено. Профиль начнет действовать в течение 30 секунд.')
    editor.value = null
    await load()
    void refreshDeviceAlerts()
  } catch (error) {
    current.error = error instanceof Error ? error.message : String(error)
  } finally {
    current.saving = false
  }
}

// setProfile меняет профиль добавленного устройства прямо в таблице.
async function setProfile(device: Device, profile: DeviceProfile): Promise<void> {
  try {
    await post('/api/devices/edit', { mac: device.mac, name: device.name, profile })
    showMessage(`${deviceTitle(device)}: профиль начнет действовать в течение 30 секунд`)
    await load()
  } catch (error) {
    showError(error)
  }
}

// remove удаляет устройство: к нему снова применяется профиль по умолчанию.
async function remove(device: Device): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить ${deviceTitle(device)}?`,
    message: `Устройство станет неизвестным: к нему будет применяться профиль по умолчанию (${defaultLabel.value}).`,
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/devices/delete', { mac: device.mac })
    showMessage('Устройство удалено')
    await load()
    void refreshDeviceAlerts()
  } catch (error) {
    showError(error)
  }
}

// saveSettings сохраняет политику для неизвестных устройств.
async function saveSettings(): Promise<void> {
  saving.value = true

  try {
    await post('/api/devices/settings', { ...form })
    showMessage('Политика сохранена. Она начнет действовать в течение 30 секунд.')
    await load(true)
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

// validatePrefix проверяет адрес или подсеть в поле-чипах.
function validatePrefix(value: string): string {
  return /^[0-9a-fA-F:.]+(\/\d{1,3})?$/.test(value) ? '' : 'Нужен IP-адрес или подсеть'
}

// seenText возвращает, когда устройство было в сети.
function seenText(device: Device): string {
  if (device.online) {
    return 'в сети'
  }

  return device.last_seen ? `был ${formatAgo(device.last_seen)}` : 'не найден в сети'
}

usePolling(() => load(), 30000)
usePolling(loadDNS, 300000)
</script>

<template>
  <Teleport to="#topbar-actions" defer>
    <button class="btn btn-secondary" :disabled="scanning" @click="scan">
      <SvgIcon class="btn-icon" :path="icons.refresh" />
      {{ scanning ? 'Поиск...' : 'Найти устройства' }}
    </button>
    <button class="btn btn-primary" @click="openNew">
      <SvgIcon class="btn-icon" :path="icons.plus" />
      Добавить по MAC
    </button>
  </Teleport>

  <p class="page-intro">
    Профили устройств сети по MAC-адресам. Приложение на устройстве может проверять, открываются ли заблокированные
    сайты, и сообщать об обходе: такому устройству лучше не давать обход. Изменения действуют через 30 секунд без
    перезапуска sing-box.
  </p>

  <div v-if="unsupported" class="callout is-bad page-callout">
    sing-box {{ state?.support.version }} не поддерживает MAC-адреса в rule-set-ах, профили устройств не действуют.
    Нужен sing-box-lx 1.14.2-lx.11-mac.1 или новее: он устанавливается при обновлении конфигуратора на странице
    <RouterLink :to="{ name: 'system-update' }">«Обновление»</RouterLink>.
  </div>
  <div v-else-if="state && !state.support.known" class="callout is-warn page-callout">
    Поддержка профилей в sing-box еще не проверена<template v-if="state.support.error">: {{ state.support.error }}</template>.
    Пока проверка не пройдет, профили устройств не попадают в конфиг.
  </div>
  <div v-else-if="state && !state.in_config" class="callout is-warn page-callout">
    Профили устройств заработают после применения конфига на странице <RouterLink :to="{ name: 'config' }">«Конфиг»</RouterLink>.
    Дальше устройства и политика меняются без перезапуска sing-box.
  </div>
  <div v-if="state?.scan_error" class="callout is-warn page-callout">Не удалось прочитать таблицу соседей хоста: {{ state.scan_error }}</div>

  <HelpHint>
    <p>
      Конфигуратор раз в минуту читает таблицу соседей хоста (<code>ip neigh</code>) и показывает найденные устройства.
      sing-box узнает MAC-адрес источника соединения сам (<code>route.find_neighbor</code>) и сверяет его со списками
      профилей, которые забирает у конфигуратора.
    </p>
    <p>
      <strong>Обход</strong> — обычная маршрутизация по группам. <strong>Без обхода</strong> — весь трафик напрямую,
      мимо правил групп; домены резолвит выбранный ниже DNS-сервер. <strong>Без интернета</strong> — соединения
      и DNS-запросы отклоняются (адреса группы bypass исключены из туннеля и остаются доступны).
    </p>
    <p>
      Имена устройств конфигуратор узнает сам: у роутера (имя, которое устройство сообщило DHCP), у самого устройства
      по mDNS (Apple, Linux, принтеры) и NetBIOS (Windows). Если имени нет, показывается производитель по MAC-адресу.
      Имена от роутера приходят через DNS sing-box, поэтому начинают находиться после применения конфига.
    </p>
    <p>MAC-адрес можно подменить, а телефоны используют случайный MAC для каждой сети Wi-Fi: профиль привязан к нему.</p>
  </HelpHint>

  <div class="section-head">
    <h3 class="section-title">Устройства с профилем</h3>
  </div>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Устройство</th>
          <th>Адрес</th>
          <th>Профиль</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="state && state.devices.length === 0">
          <td colspan="4" class="empty-state">Устройств с профилем нет: ко всем применяется профиль по умолчанию ({{ defaultLabel }}).</td>
        </tr>
        <tr v-for="device in state?.devices ?? []" :key="device.mac">
          <td>
            <span class="cell-main" :title="deviceHint(device) || undefined">{{ deviceTitle(device) }}</span>
            <span v-if="device.random_mac" class="status-badge status-pending device-badge" :title="deviceHint(device)">случайный MAC</span>
            <div class="cell-sub">{{ deviceDetails(device) }}</div>
          </td>
          <td data-label="Адрес">
            <span class="cell-mono">{{ device.ips.join(', ') || '—' }}</span>
            <div class="cell-sub">{{ seenText(device) }}</div>
          </td>
          <td data-label="Профиль">
            <select
              class="form-select"
              :value="device.profile"
              :aria-label="`Профиль ${deviceTitle(device)}`"
              @change="setProfile(device, ($event.target as HTMLSelectElement).value as DeviceProfile)"
            >
              <option v-for="option in profileOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </td>
          <td class="actions-cell">
            <div class="row-actions">
              <IconButton icon="edit" title="Изменить" @click="openEditor(device, true)" />
              <IconButton icon="trash" title="Удалить" danger @click="remove(device)" />
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="section-head">
    <h3 class="section-title">Новые устройства</h3>
  </div>
  <p class="section-hint">
    Найдены в сети, профиль не задан: действует профиль по умолчанию ({{ defaultLabel }}). Добавьте устройство, чтобы
    дать ему свой профиль или просто отметить как знакомое.
  </p>

  <div class="data-table">
    <table class="table table-cards">
      <thead>
        <tr>
          <th>Устройство</th>
          <th>Адрес</th>
          <th>Впервые</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="state && state.unknown.length === 0">
          <td colspan="4" class="empty-state">Новых устройств нет.</td>
        </tr>
        <tr v-for="device in state?.unknown ?? []" :key="device.mac">
          <td>
            <span class="cell-main" :class="{ 'cell-mono': deviceTitle(device) === device.mac }" :title="deviceHint(device) || undefined">
              {{ deviceTitle(device) }}
            </span>
            <span v-if="device.random_mac" class="status-badge status-pending device-badge" :title="deviceHint(device)">случайный MAC</span>
            <div class="cell-sub">{{ deviceDetails(device) }}</div>
          </td>
          <td data-label="Адрес">
            <span class="cell-mono">{{ device.ips.join(', ') || '—' }}</span>
            <div class="cell-sub">{{ seenText(device) }}</div>
          </td>
          <td data-label="Впервые">{{ formatAgo(device.first_seen) }}</td>
          <td class="actions-cell">
            <div class="row-actions">
              <button class="btn btn-secondary btn-sm" @click="openEditor(device, false)">Добавить</button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="section-head">
    <h3 class="section-title">Политика для неизвестных устройств</h3>
  </div>
  <p class="section-hint">Профиль для устройств без своего профиля, в том числе только что подключившихся к сети.</p>

  <form @submit.prevent="saveSettings">
    <div class="settings-card">
      <SettingRow title="Профиль по умолчанию">
        <template #description>{{ profileHints[form.default_profile] }}.</template>
        <SegmentedControl v-model="form.default_profile" :options="defaultOptions" aria-label="Профиль по умолчанию" />
        <span v-if="noNetworks" class="field-warning">Сети LAN не найдены и не заданы: профиль по умолчанию не действует.</span>
      </SettingRow>

      <SettingRow title="Находить сети LAN">
        <template #description>
          Профиль по умолчанию действует только для адресов из сетей LAN: подсетей интерфейсов, где есть соседи
          (кроме docker, VPN и туннелей). Найдено: {{ state?.detected.networks.join(', ') || 'ничего' }}.<template v-if="state?.detected.gateway"> Роутер: {{ state.detected.gateway }}.</template>
        </template>
        <ToggleSwitch v-model="form.auto_networks" :label="form.auto_networks ? 'Автоматически' : 'Только заданные'" />
      </SettingRow>

      <SettingRow title="Сети LAN" input-id="deviceNetworks" class="is-top">
        <template #description>Дополнительные подсети, например за другим роутером.</template>
        <ChipsInput v-model="form.networks" input-id="deviceNetworks" placeholder="192.168.1.0/24" :validate="validatePrefix" />
      </SettingRow>

      <SettingRow title="Исключения" input-id="deviceExclude" class="is-top">
        <template #description>
          Адреса и подсети, к которым профиль по умолчанию не применяется (серверы, VPN-клиенты). Адреса самого хоста
          исключены всегда: {{ state?.detected.host_addresses.join(', ') || 'не найдены' }}.
        </template>
        <ChipsInput v-model="form.exclude" input-id="deviceExclude" placeholder="192.168.1.10" :validate="validatePrefix" />
      </SettingRow>

      <SettingRow title="DNS для устройств без обхода" input-id="deviceDNS">
        <template #description>
          Резолвит все домены устройств с профилем «Без обхода». Выберите сервер без detour через VPN, например
          провайдера. Не задан — действуют общие DNS-правила. Меняется применением конфига.
        </template>
        <select id="deviceDNS" v-model="form.direct_dns_server" class="form-select">
          <option value="">Не задан</option>
          <option v-for="server in dnsServers" :key="server.tag" :value="server.tag">
            {{ server.tag }}{{ server.detour ? ` (через ${server.detour})` : '' }}
          </option>
        </select>
      </SettingRow>
    </div>

    <SaveBar v-if="state" class="is-floating" :dirty="dirty" :saving="saving" @reset="reset" />
  </form>

  <ModalDialog v-if="editor" :title="editor.editing ? `Устройство ${editor.name || editor.mac}` : 'Новое устройство'" @close="editor = null">
    <form class="form-stack" @submit.prevent="saveDevice">
      <FormField label="MAC-адрес" input-id="deviceMac" :hint="editor.editing ? 'MAC не меняется после добавления.' : 'Например, aa:bb:cc:dd:ee:ff.'">
        <input
          id="deviceMac"
          v-model="editor.mac"
          class="form-input is-mono"
          type="text"
          required
          autocomplete="off"
          spellcheck="false"
          :disabled="editor.editing"
        >
      </FormField>

      <FormField label="Имя" input-id="deviceName" optional>
        <input id="deviceName" v-model="editor.name" class="form-input" type="text" maxlength="64" autocomplete="off" placeholder="Телевизор в гостиной">
      </FormField>

      <div class="field">
        <span class="field-label">Профиль</span>
        <SegmentedControl v-model="editor.profile" :options="profileOptions" aria-label="Профиль устройства" />
        <p class="field-hint">{{ editor.profile === 'default' ? 'Как у неизвестных устройств.' : profileHints[editor.profile] }}</p>
      </div>

      <div v-if="editor.error" class="form-error">{{ editor.error }}</div>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="editor = null">Отмена</button>
        <button type="submit" class="btn btn-primary" :disabled="editor.saving">
          {{ editor.saving ? 'Сохранение...' : 'Сохранить' }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>

<style scoped>
.page-callout {
  margin-bottom: 16px;
}

.device-badge {
  margin-left: 6px;
}
</style>
