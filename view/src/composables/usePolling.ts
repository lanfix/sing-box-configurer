// Периодический вызов функции, пока страница открыта.

import { onBeforeUnmount, onMounted } from 'vue'

// usePolling вызывает fn сразу при монтировании страницы и затем каждые intervalMs.
export function usePolling(fn: () => unknown, intervalMs: number): void {
  let timer: ReturnType<typeof setInterval> | null = null

  onMounted(() => {
    void fn()
    timer = setInterval(() => void fn(), intervalMs)
  })

  onBeforeUnmount(() => {
    if (timer) {
      clearInterval(timer)
    }
  })
}
