// Проверка значений новых правил на пересечения до добавления (с задержкой после ввода).

import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

import { postQuiet } from '../api/client'
import type { Rule, RuleCheck } from '../api/types'

// checkDelay — пауза после ввода перед проверкой, мс.
const checkDelay = 300

// maxChecked — сколько значений проверять на лету (большие списки проверяются при добавлении).
const maxChecked = 500

// useRuleCheck проверяет значения values типа type группы group и возвращает результаты по порядку значений
// и число неприменённых изменений правил.
export function useRuleCheck(type: Ref<Rule['type']>, values: Ref<string[]>, group: Ref<string>) {
  const checks = ref<RuleCheck[]>([])
  const pendingCount = ref(0)
  const checking = ref(false)

  let timer: ReturnType<typeof setTimeout> | null = null
  let sequence = 0

  // run отправляет проверку. Ответ на устаревший запрос игнорируется.
  async function run(): Promise<void> {
    const current = ++sequence
    const list = values.value.slice(0, maxChecked)

    if (list.length === 0 || !group.value) {
      checks.value = []
      checking.value = false

      return
    }

    checking.value = true

    try {
      const data = await postQuiet<{ checks: RuleCheck[]; pending_count: number }>('/api/rules/check', {
        type: type.value,
        values: list,
        group: group.value,
      })

      if (current === sequence) {
        checks.value = data.checks ?? []
        pendingCount.value = data.pending_count ?? 0
      }
    } catch {
      // Проверка — подсказка: при ошибке сервер все равно проверит значения при добавлении.
      if (current === sequence) {
        checks.value = []
      }
    } finally {
      if (current === sequence) {
        checking.value = false
      }
    }
  }

  watch([type, values, group], () => {
    if (timer) {
      clearTimeout(timer)
    }

    checking.value = values.value.length > 0
    timer = setTimeout(() => void run(), checkDelay)
  }, { deep: true, immediate: true })

  onBeforeUnmount(() => {
    if (timer) {
      clearTimeout(timer)
    }
  })

  return { checks, pendingCount, checking, recheck: run }
}
