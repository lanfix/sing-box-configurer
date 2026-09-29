// Отслеживание несохраненных изменений форм настроек.

import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'

import { confirmAction } from '../stores/confirm'

// useSavedState сравнивает реактивную форму с последним сохраненным состоянием.
export function useSavedState<T extends object>(form: T) {
  const saved = ref(JSON.stringify(form))
  const dirty = computed(() => JSON.stringify(form) !== saved.value)

  // markSaved запоминает текущее состояние формы как сохраненное.
  function markSaved(): void {
    saved.value = JSON.stringify(form)
  }

  // reset возвращает форму к сохраненному состоянию.
  function reset(): void {
    Object.assign(form, JSON.parse(saved.value))
  }

  return { dirty, markSaved, reset }
}

// useLeaveGuard просит подтверждение, если со страницы уходят с несохраненными изменениями.
export function useLeaveGuard(dirty: () => boolean): void {
  // onBeforeUnload предупреждает о несохраненных изменениях при закрытии вкладки.
  function onBeforeUnload(event: BeforeUnloadEvent): void {
    if (dirty()) {
      event.preventDefault()
    }
  }

  onBeforeRouteLeave(async () => {
    if (!dirty()) {
      return true
    }

    return confirmAction({
      title: 'Уйти без сохранения?',
      message: 'На странице есть несохраненные изменения, они будут потеряны.',
      confirmText: 'Уйти',
      danger: true,
    })
  })

  onMounted(() => window.addEventListener('beforeunload', onBeforeUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))
}
