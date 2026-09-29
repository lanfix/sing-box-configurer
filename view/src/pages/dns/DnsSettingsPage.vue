<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { get, post } from '../../api/client'
import type { DNSData, DNSServer } from '../../api/types'
import DurationInput from '../../components/ui/DurationInput.vue'
import SaveBar from '../../components/ui/SaveBar.vue'
import SettingRow from '../../components/ui/SettingRow.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useLeaveGuard, useSavedState } from '../../composables/useSavedState'
import { showError, showMessage } from '../../stores/toast'

// strategies — какие адреса запрашивать у DNS-серверов.
const strategies = [
  { value: '', label: 'По умолчанию sing-box' },
  { value: 'ipv4_only', label: 'Только IPv4' },
  { value: 'prefer_ipv4', label: 'Сначала IPv4, затем IPv6' },
  { value: 'prefer_ipv6', label: 'Сначала IPv6, затем IPv4' },
  { value: 'ipv6_only', label: 'Только IPv6' },
]

const servers = ref<DNSServer[]>([])
const loaded = ref(false)
const saving = ref(false)

const form = reactive({
  final: '',
  strategy: '',
  default_domain_resolver: '',
  cache_capacity: '' as number | '',
  optimistic: false,
  timeout: '',
})

const { dirty, markSaved, reset } = useSavedState(form)

useLeaveGuard(() => dirty.value)

// serverLabel возвращает подпись сервера для выпадающего списка.
function serverLabel(server: DNSServer): string {
  return server.description ? `${server.tag} — ${server.description}` : server.tag
}

// resolverWarning — резолвер outbound-ов сам ходит через outbound: sing-box не сможет узнать адрес VPN-сервера.
const resolverWarning = computed(() => {
  const resolver = servers.value.find((server) => server.tag === form.default_domain_resolver)

  return resolver?.detour ? `Сервер ${resolver.tag} отправляет запросы через ${resolver.detour}: если это VPN, адреса VPN-серверов не узнать, пока VPN не подключен.` : ''
})

// load загружает настройки DNS.
async function load(): Promise<void> {
  try {
    const data = await get<DNSData>('/api/dns')

    servers.value = data.servers
    form.final = data.settings.final ?? ''
    form.strategy = data.settings.strategy ?? ''
    form.default_domain_resolver = data.settings.default_domain_resolver ?? ''
    form.cache_capacity = data.settings.cache_capacity || ''
    form.optimistic = data.settings.optimistic ?? false
    form.timeout = data.settings.timeout ?? ''
    markSaved()
  } catch (error) {
    showError(error, 'Ошибка загрузки настроек DNS')
  } finally {
    loaded.value = true
  }
}

// save сохраняет общие параметры DNS.
async function save(): Promise<void> {
  saving.value = true

  try {
    await post('/api/dns/settings', {
      final: form.final,
      strategy: form.strategy,
      default_domain_resolver: form.default_domain_resolver,
      cache_capacity: Number(form.cache_capacity) || 0,
      optimistic: form.optimistic,
      timeout: form.timeout,
    })

    showMessage('Настройки DNS сохранены')
    await load()
  } catch (error) {
    showError(error)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <form class="page-narrow" @submit.prevent="save">
    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Резолвинг</div>
        <p class="settings-card-description">Куда уходят DNS-запросы и какие адреса запрашивать.</p>
      </div>

      <SettingRow title="Сервер по умолчанию" input-id="dnsFinal">
        <template #description>
          Отвечает на все запросы, кроме доменов из <RouterLink :to="{ name: 'groups' }">групп</RouterLink> со своим
          DNS-сервером и <RouterLink :to="{ name: 'dns-records' }">DNS-записей</RouterLink>.
        </template>
        <select id="dnsFinal" v-model="form.final" class="form-select">
          <option value="">Первый в списке{{ servers[0] ? ` (${servers[0].tag})` : '' }}</option>
          <option v-for="server in servers" :key="server.tag" :value="server.tag">{{ serverLabel(server) }}</option>
        </select>
      </SettingRow>

      <SettingRow title="Резолвер адресов outbound-ов" input-id="dnsResolver" class="is-top">
        <template #description>
          Через этот сервер sing-box узнает IP-адреса VPN-серверов, заданных доменом (например, <code>vpn.example.com</code>).
          Выберите сервер, который работает напрямую.
        </template>
        <select id="dnsResolver" v-model="form.default_domain_resolver" class="form-select">
          <option value="">Не задан</option>
          <option v-for="server in servers" :key="server.tag" :value="server.tag">{{ serverLabel(server) }}</option>
        </select>
        <span v-if="resolverWarning" class="field-warning">{{ resolverWarning }}</span>
      </SettingRow>

      <SettingRow title="IP-версия" input-id="dnsStrategy">
        <template #description>
          Какие адреса запрашивать у DNS-серверов. Если провайдер не дает IPv6, выберите «Только IPv4» — так не будет
          долгих попыток подключиться по IPv6.
        </template>
        <select id="dnsStrategy" v-model="form.strategy" class="form-select">
          <option v-for="item in strategies" :key="item.value" :value="item.value">{{ item.label }}</option>
        </select>
      </SettingRow>

      <SettingRow title="Таймаут запроса" input-id="dnsTimeout" description="Сколько ждать ответа DNS-сервера. Пусто — значение sing-box по умолчанию.">
        <DurationInput v-model="form.timeout" input-id="dnsTimeout" :units="['ms', 's']" placeholder="по умолчанию" />
      </SettingRow>
    </div>

    <div class="settings-card">
      <div class="settings-card-head">
        <div class="settings-card-title">Кэш</div>
        <p class="settings-card-description">Повторные запросы к тем же доменам sing-box отдает из кэша, не обращаясь к серверу.</p>
      </div>

      <SettingRow title="Размер кэша" input-id="dnsCache" description="Сколько ответов хранить. Значения меньше 1024 sing-box игнорирует. Пусто — по умолчанию.">
        <div class="input-group">
          <input id="dnsCache" v-model.number="form.cache_capacity" class="form-input" type="number" min="0" step="1024" placeholder="по умолчанию">
          <span class="input-addon">записей</span>
        </div>
      </SettingRow>

      <SettingRow title="Оптимистичный кэш">
        <template #description>
          Устаревшая запись отдается из кэша сразу, а обновляется в фоне. Сайты открываются быстрее, но после смены
          адреса сайта первый запрос может получить старый адрес.
        </template>
        <ToggleSwitch v-model="form.optimistic" :label="form.optimistic ? 'Включен' : 'Выключен'" />
      </SettingRow>
    </div>

    <p class="field-hint" style="margin-bottom: 16px;">
      Правила sing-box и параметры, которых нет в форме, задаются JSON-ом на странице
      <RouterLink :to="{ name: 'dns-advanced' }">«Расширенные»</RouterLink>.
    </p>

    <SaveBar v-if="loaded" class="is-floating" :dirty="dirty" :saving="saving" @reset="reset" />
  </form>
</template>
