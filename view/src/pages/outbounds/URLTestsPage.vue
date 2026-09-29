<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { get, post, postQuiet } from '../../api/client'
import type {
  SubscriptionProfileRef,
  URLTest,
  URLTestCandidate,
  URLTestMember,
  URLTestSource,
  URLTestsData,
  URLTestView,
} from '../../api/types'
import ModalDialog from '../../components/ModalDialog.vue'
import SvgIcon from '../../components/SvgIcon.vue'
import DurationInput from '../../components/ui/DurationInput.vue'
import FormField from '../../components/ui/FormField.vue'
import HelpHint from '../../components/ui/HelpHint.vue'
import IconButton from '../../components/ui/IconButton.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import { useGroups } from '../../composables/useGroups'
import { icons } from '../../icons'
import { confirmAction } from '../../stores/confirm'
import { showError, showMessage } from '../../stores/toast'

// SourceOption — вариант источника в форме.
interface SourceOption {
  key: string
  label: string
  source: URLTestSource
  nested: boolean
}

const { groups, loadGroups } = useGroups()

const urlTests = ref<URLTestView[]>([])
const candidates = ref<URLTestCandidate[]>([])
const profiles = ref<SubscriptionProfileRef[]>([])
const loaded = ref(false)

// expanded — теги urltest-ов, у которых в таблице раскрыт состав.
const expanded = ref<string[]>([])

// Форма urltest-а: id пустой при добавлении.
const form = ref<URLTest | null>(null)
const preview = ref<URLTestMember[]>([])
const previewError = ref('')
const saveError = ref('')
const addTag = ref('')

// sourceTitles — подписи источников.
const sourceTitles: Record<URLTestSource['kind'], string> = {
  all: 'Все outbound-ы',
  manual: 'Добавленные вручную',
  happ: 'Happ',
  amnezia: 'Amnezia',
}

// stateTitles — пояснения к состояниям участников.
const stateTitles: Record<URLTestMember['state'], string> = {
  included: 'Входит в URLTest',
  excluded_tag: 'Исключен вручную',
  excluded_regexp: 'Исключен выражением',
  missing: 'Outbound не найден',
}

// sourceKey возвращает ключ источника для сравнения.
function sourceKey(source: URLTestSource): string {
  return source.profile_id ? `${source.kind}:${source.profile_id}` : source.kind
}

// profileName возвращает имя профиля подписки или пустую строку, если профиля нет.
function profileName(kind: string, id?: string): string {
  return profiles.value.find((profile) => profile.source === kind && profile.id === id)?.name ?? ''
}

// sourceLabel возвращает подпись источника urltest-а.
function sourceLabel(source: URLTestSource): string {
  if (!source.profile_id) {
    return source.kind === 'happ' || source.kind === 'amnezia' ? `${sourceTitles[source.kind]}: все профили` : sourceTitles[source.kind]
  }

  return `${sourceTitles[source.kind]}: ${profileName(source.kind, source.profile_id) || 'профиль удален'}`
}

// candidateLabel возвращает источник outbound-а для подписи участника.
function candidateLabel(tag: string): string {
  const candidate = candidates.value.find((item) => item.tag === tag)

  if (!candidate) {
    return ''
  }

  const name = profileName(candidate.source, candidate.profile_id)

  return name ? `${sourceTitles[candidate.source]} · ${name}` : sourceTitles[candidate.source]
}

// sourceOptions — варианты источников: все, вручную, подписки целиком, их профили и источники удаленных профилей.
const sourceOptions = computed<SourceOption[]>(() => {
  const options: SourceOption[] = [
    { key: 'all', label: sourceTitles.all, source: { kind: 'all' }, nested: false },
    { key: 'manual', label: sourceTitles.manual, source: { kind: 'manual' }, nested: false },
  ]

  for (const kind of ['happ', 'amnezia'] as const) {
    options.push({ key: kind, label: `${sourceTitles[kind]}: все профили`, source: { kind }, nested: false })

    for (const profile of profiles.value.filter((item) => item.source === kind)) {
      options.push({ key: `${kind}:${profile.id}`, label: profile.name, source: { kind, profile_id: profile.id }, nested: true })
    }
  }

  for (const source of form.value?.sources ?? []) {
    if (!options.some((option) => option.key === sourceKey(source))) {
      options.push({ key: sourceKey(source), label: sourceLabel(source), source, nested: true })
    }
  }

  return options
})

// selectedSources — ключи выбранных источников.
const selectedSources = computed(() => (form.value?.sources ?? []).map(sourceKey))

// sourceCovered проверяет, что вариант уже входит в выбранный более широкий источник.
function sourceCovered(option: SourceOption): boolean {
  if (option.key === 'all') {
    return false
  }

  return selectedSources.value.includes('all') || (option.nested && selectedSources.value.includes(option.source.kind))
}

// toggleSource добавляет источник в форму или убирает его.
function toggleSource(option: SourceOption): void {
  if (!form.value) {
    return
  }

  if (selectedSources.value.includes(option.key)) {
    form.value.sources = form.value.sources.filter((source) => sourceKey(source) !== option.key)
  } else {
    form.value.sources = [...form.value.sources, option.source]
  }
}

// includedCount — сколько outbound-ов войдет в urltest по превью.
const includedCount = computed(() => preview.value.filter((member) => member.state === 'included').length)

// addableCandidates — outbound-ы, которых еще нет в составе: их можно добавить явно.
const addableCandidates = computed(() => candidates.value.filter((candidate) => !preview.value.some((member) => member.tag === candidate.tag)))

// groupsUsing возвращает группы, у которых urltest выбран outbound-ом по умолчанию.
function groupsUsing(tag: string): string[] {
  return groups.value.filter((group) => !group.system && group.default_outbound === tag).map((group) => group.name)
}

// includedMembers возвращает участников, которые войдут в urltest.
function includedMembers(view: URLTestView): URLTestMember[] {
  return view.members.filter((member) => member.state === 'included')
}

// load загружает urltest-ы, outbound-ы для формы и группы.
async function load(): Promise<void> {
  try {
    const [data] = await Promise.all([
      get<URLTestsData>('/api/urltests'),
      loadGroups(),
    ])

    urlTests.value = data.urltests ?? []
    candidates.value = data.candidates ?? []
    profiles.value = data.profiles ?? []
  } catch (error) {
    showError(error, 'Ошибка загрузки URLTest-ов')
  } finally {
    loaded.value = true
  }
}

// toggleExpanded раскрывает или сворачивает состав urltest-а в таблице.
function toggleExpanded(tag: string): void {
  expanded.value = expanded.value.includes(tag) ? expanded.value.filter((item) => item !== tag) : [...expanded.value, tag]
}

// openEditor открывает форму нового или существующего urltest-а.
function openEditor(view?: URLTestView): void {
  form.value = view
    ? {
        id: view.id,
        tag: view.tag,
        description: view.description,
        sources: (view.sources ?? []).map((source) => ({ ...source })),
        include_regexp: view.include_regexp ?? '',
        exclude_regexp: view.exclude_regexp ?? '',
        tags: [...(view.tags ?? [])],
        exclude_tags: [...(view.exclude_tags ?? [])],
        url: view.url ?? '',
        interval: view.interval ?? '',
        tolerance: view.tolerance ?? 0,
        interrupt_exist_connections: view.interrupt_exist_connections,
      }
    : {
        id: '',
        tag: '',
        description: '',
        sources: [],
        include_regexp: '',
        exclude_regexp: '',
        tags: [],
        exclude_tags: [],
        url: 'https://www.gstatic.com/generate_204',
        interval: '3m',
        tolerance: 100,
        interrupt_exist_connections: false,
      }

  preview.value = view?.members ?? []
  previewError.value = view?.error ?? ''
  saveError.value = ''
  addTag.value = ''
}

// payload возвращает тело запроса из формы: пустой tolerance отправляется нулем.
function payload(current: URLTest): URLTest {
  return {
    ...current,
    tolerance: Number(current.tolerance) || 0,
  }
}

let previewTimer: ReturnType<typeof setTimeout> | null = null
let previewSeq = 0

// runPreview запрашивает состав urltest-а по текущей форме. Устаревшие ответы отбрасываются.
async function runPreview(): Promise<void> {
  if (!form.value) {
    return
  }

  const seq = ++previewSeq

  try {
    const result = await postQuiet<{ members: URLTestMember[]; error?: string }>('/api/urltests/preview', payload(form.value))

    if (seq === previewSeq) {
      preview.value = result.members ?? []
      previewError.value = result.error ?? ''
    }
  } catch (error) {
    if (seq === previewSeq) {
      previewError.value = error instanceof Error ? error.message : String(error)
    }
  }
}

// Превью обновляется с небольшой задержкой, чтобы не запрашивать его на каждый символ выражения.
watch(form, () => {
  if (previewTimer) {
    clearTimeout(previewTimer)
  }

  previewTimer = setTimeout(() => {
    previewTimer = null
    void runPreview()
  }, 250)
}, { deep: true })

// toggleMember исключает участника или возвращает исключенного вручную.
function toggleMember(member: URLTestMember): void {
  if (!form.value) {
    return
  }

  if (member.state === 'included') {
    form.value.exclude_tags = [...form.value.exclude_tags, member.tag]
  } else if (member.state === 'excluded_tag') {
    form.value.exclude_tags = form.value.exclude_tags.filter((tag) => tag !== member.tag)
  }
}

// includeAll снимает все ручные исключения.
function includeAll(): void {
  if (form.value) {
    form.value.exclude_tags = []
  }
}

// addExplicit добавляет выбранный outbound явно.
function addExplicit(): void {
  if (!form.value || !addTag.value) {
    return
  }

  form.value.tags = [...form.value.tags, addTag.value]
  form.value.exclude_tags = form.value.exclude_tags.filter((tag) => tag !== addTag.value)
  addTag.value = ''
}

// removeExplicit убирает явно добавленный outbound.
function removeExplicit(tag: string): void {
  if (form.value) {
    form.value.tags = form.value.tags.filter((item) => item !== tag)
  }
}

// save сохраняет urltest.
async function save(): Promise<void> {
  const current = form.value

  if (!current) {
    return
  }

  try {
    await post(current.id ? '/api/urltests/edit' : '/api/urltests/add', payload(current))
    showMessage(`URLTest ${current.tag} сохранен`)
    form.value = null
    await load()
  } catch (error) {
    saveError.value = error instanceof Error ? error.message : String(error)
  }
}

// remove удаляет urltest.
async function remove(view: URLTestView): Promise<void> {
  const confirmed = await confirmAction({
    title: `Удалить URLTest ${view.tag}?`,
    message: 'Он пропадет из итогового конфига.',
    confirmText: 'Удалить',
    danger: true,
  })

  if (!confirmed) {
    return
  }

  try {
    await post('/api/urltests/delete', { id: view.id })
    showMessage(`URLTest ${view.tag} удален`)
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
      Добавить URLTest
    </button>
  </Teleport>

  <p class="page-intro">
    URLTest регулярно замеряет задержку своих серверов и сам выбирает самый быстрый. Его можно выбрать как outbound
    по умолчанию в <RouterLink :to="{ name: 'groups' }">группе</RouterLink>.
  </p>

  <HelpHint>
    <p>
      Состав собирается при каждом рендере конфига: серверы выбранных источников (например, всей подписки Happ или
      одного ее профиля), подходящие под фильтр, плюс явно добавленные, минус исключенные — вручную или выражением.
      Новые серверы подписки попадают в URLTest автоматически.
    </p>
    <p>Пустой URLTest в конфиг не попадает, а группы, выбравшие его, получают block.</p>
  </HelpHint>

  <div class="data-table">
    <table class="table">
      <thead>
        <tr>
          <th>Тег</th>
          <th>Источники и фильтры</th>
          <th>Состав</th>
          <th>Используют группы</th>
          <th class="col-actions"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loaded && urlTests.length === 0">
          <td colspan="5" class="empty-state">URLTest-ов нет. Добавьте первый кнопкой «Добавить URLTest».</td>
        </tr>
        <template v-for="view in urlTests" :key="view.id">
          <tr>
            <td>
              <span class="cell-main">{{ view.tag }}</span>
              <div v-if="view.description" class="cell-sub">{{ view.description }}</div>
            </td>
            <td>
              <div class="chip-list">
                <span v-for="source in view.sources ?? []" :key="sourceKey(source)" class="badge badge-source">{{ sourceLabel(source) }}</span>
                <span v-if="view.tags?.length" class="badge badge-source">+{{ view.tags.length }} явно</span>
              </div>
              <div v-if="view.include_regexp" class="urltest-filter">фильтр: <code>{{ view.include_regexp }}</code></div>
              <div v-if="view.exclude_regexp" class="urltest-filter">исключить: <code>{{ view.exclude_regexp }}</code></div>
              <div v-if="view.exclude_tags?.length" class="urltest-filter">исключено вручную: {{ view.exclude_tags.length }}</div>
            </td>
            <td>
              <span v-if="view.error" class="status-badge status-error" :title="view.error">ошибка</span>
              <template v-else>
                <span
                  v-if="includedMembers(view).length === 0"
                  class="status-badge status-error"
                  title="Пустой URLTest не попадет в конфиг"
                >пуст</span>
                <span v-else>{{ includedMembers(view).length }} outbound-ов</span>
                <button v-if="view.members.length" type="button" class="link-button" style="margin-left: 8px;" @click="toggleExpanded(view.tag)">
                  {{ expanded.includes(view.tag) ? 'скрыть' : 'показать' }}
                </button>
              </template>
            </td>
            <td>
              <span v-if="groupsUsing(view.tag).length">{{ groupsUsing(view.tag).join(', ') }}</span>
              <span v-else class="muted">—</span>
            </td>
            <td class="actions-cell">
              <div class="row-actions">
                <IconButton icon="edit" title="Изменить" @click="openEditor(view)" />
                <IconButton
                  icon="trash"
                  danger
                  :title="groupsUsing(view.tag).length ? `Нельзя удалить: выбран в группах ${groupsUsing(view.tag).join(', ')}` : 'Удалить'"
                  :disabled="groupsUsing(view.tag).length > 0"
                  @click="remove(view)"
                />
              </div>
            </td>
          </tr>
          <tr v-if="expanded.includes(view.tag)">
            <td colspan="5">
              <div class="chip-list">
                <span
                  v-for="member in view.members"
                  :key="member.tag"
                  class="chip"
                  :class="{ 'chip-off': member.state === 'excluded_tag' || member.state === 'excluded_regexp', 'chip-missing': member.state === 'missing' }"
                  :title="`${stateTitles[member.state]}${candidateLabel(member.tag) ? ' · ' + candidateLabel(member.tag) : ''}`"
                >{{ member.tag }}</span>
              </div>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="form" :title="form.id ? `URLTest ${form.tag}` : 'Новый URLTest'" wide @close="form = null">
    <form class="form-stack" @submit.prevent="save">
      <div class="form-grid">
        <FormField
          label="Тег"
          input-id="urltestTag"
          :hint="form.id ? 'Не меняется: на тег ссылаются группы.' : 'Короткое имя латиницей, под ним URLTest виден в группах.'"
        >
          <input
            id="urltestTag"
            v-model="form.tag"
            class="form-input"
            type="text"
            placeholder="happ-fast"
            required
            autocomplete="off"
            spellcheck="false"
            :disabled="Boolean(form.id)"
          >
        </FormField>
        <FormField label="Описание" input-id="urltestDescription" optional>
          <input id="urltestDescription" v-model="form.description" class="form-input" type="text" placeholder="Самый быстрый сервер Happ">
        </FormField>
      </div>

      <div class="field">
        <span class="field-label">Откуда брать серверы</span>
        <div class="source-list">
          <label
            v-for="option in sourceOptions"
            :key="option.key"
            class="check-label"
            :class="{ 'source-nested': option.nested, 'source-covered': sourceCovered(option) && !selectedSources.includes(option.key) }"
          >
            <input type="checkbox" :checked="selectedSources.includes(option.key)" @change="toggleSource(option)">
            {{ option.label }}
          </label>
        </div>
      </div>

      <div class="form-grid">
        <FormField label="Брать только теги, где есть" input-id="urltestInclude" optional>
          <input id="urltestInclude" v-model="form.include_regexp" class="form-input is-mono" type="text" placeholder="(?i)germany|de-" spellcheck="false">
        </FormField>
        <FormField label="Исключить теги, где есть" input-id="urltestExclude" optional>
          <input id="urltestExclude" v-model="form.exclude_regexp" class="form-input is-mono" type="text" placeholder="(?i)ru|test" spellcheck="false">
        </FormField>
        <p class="field-hint span-2" style="margin-top: -10px;">
          Регулярные выражения RE2 (Go), ищутся в любой части тега; <code>(?i)</code> — без учета регистра. Фильтр
          применяется к серверам источников, исключение — ко всем. selector-ы и URLTest-ы из источников не подбираются —
          их можно добавить явно.
        </p>
      </div>

      <div class="field">
        <div class="urltest-preview-head">
          <span class="field-label">Состав: {{ includedCount }} из {{ preview.length }}</span>
          <button v-if="form.exclude_tags.length" type="button" class="link-button" @click="includeAll">вернуть исключенные вручную</button>
        </div>
        <p v-if="previewError" class="field-error">{{ previewError }}</p>
        <div v-else-if="preview.length === 0" class="member-list member-empty muted">
          Нет подходящих серверов: выберите источник или добавьте сервер явно.
        </div>
        <div v-else class="member-list">
          <label
            v-for="member in preview"
            :key="member.tag"
            class="member-row"
            :class="{ 'is-off': member.state !== 'included' }"
            :title="stateTitles[member.state]"
          >
            <input
              type="checkbox"
              :checked="member.state === 'included'"
              :disabled="member.state === 'excluded_regexp' || member.state === 'missing'"
              @change="toggleMember(member)"
            >
            <span class="member-tag">{{ member.tag }}</span>
            <span v-if="member.state === 'excluded_regexp'" class="muted">исключен выражением</span>
            <span v-if="member.state === 'missing'" class="status-badge status-error">не найден</span>
            <span class="muted member-source">{{ candidateLabel(member.tag) }}</span>
            <template v-if="form.tags.includes(member.tag)">
              <span class="badge badge-source">явно</span>
              <IconButton icon="close" title="Убрать из явно добавленных" class="member-remove" @click.prevent="removeExplicit(member.tag)" />
            </template>
          </label>
        </div>
        <p class="field-hint">Снимите галочку, чтобы исключить сервер вручную.</p>
      </div>

      <FormField label="Добавить сервер явно" input-id="urltestAddTag" hint="Например, сервер из источника, который не выбран выше, или другой URLTest." optional>
        <div class="input-group">
          <select id="urltestAddTag" v-model="addTag" class="form-select" style="flex: 1; width: auto;">
            <option value="">Выберите outbound</option>
            <option v-for="candidate in addableCandidates" :key="candidate.tag" :value="candidate.tag">
              {{ candidate.tag }} ({{ candidate.type }}, {{ candidateLabel(candidate.tag) }})
            </option>
          </select>
          <button type="button" class="btn btn-secondary" :disabled="!addTag" @click="addExplicit">Добавить</button>
        </div>
      </FormField>

      <details class="disclosure">
        <summary>
          <SvgIcon :path="icons.chevron" />
          Параметры проверки
          <span class="disclosure-note">— адрес, частота, порог переключения</span>
        </summary>
        <div class="disclosure-body form-stack">
          <FormField label="Адрес проверки" input-id="urltestURL" optional hint="Запрос к нему через каждый сервер измеряет задержку.">
            <input id="urltestURL" v-model="form.url" class="form-input" type="url" placeholder="по умолчанию sing-box" spellcheck="false">
          </FormField>
          <div class="form-grid">
            <FormField label="Проверять каждые" input-id="urltestInterval" optional>
              <DurationInput :model-value="form.interval ?? ''" input-id="urltestInterval" :units="['s', 'm', 'h']" default-unit="m" placeholder="3" @update:model-value="form!.interval = $event" />
            </FormField>
            <FormField label="Порог переключения" input-id="urltestTolerance" optional hint="Сервер сменится, только если новый быстрее на столько.">
              <div class="input-group">
                <input id="urltestTolerance" v-model.number="form.tolerance" class="form-input" type="number" min="0" placeholder="50">
                <span class="input-addon">мс</span>
              </div>
            </FormField>
          </div>
          <ToggleSwitch
            v-model="form.interrupt_exist_connections"
            label="Разрывать соединения при смене сервера"
            description="Открытые соединения сразу переходят на новый сервер. Без этого они доживают на старом."
          />
        </div>
      </details>

      <div v-if="saveError" class="form-error">{{ saveError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-secondary" @click="form = null">Отмена</button>
        <button type="submit" class="btn btn-primary">{{ form.id ? 'Сохранить' : 'Добавить URLTest' }}</button>
      </div>
    </form>
  </ModalDialog>
</template>
