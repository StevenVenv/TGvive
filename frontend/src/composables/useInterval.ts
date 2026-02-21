import { onBeforeUnmount, ref } from 'vue'

export function useInterval(fn: () => void, delayMs: number) {
  const timer = ref<number | undefined>(undefined)
  const delay = Math.max(100, Math.floor(delayMs))

  function stop() {
    if (timer.value) window.clearInterval(timer.value)
    timer.value = undefined
  }

  function start() {
    stop()
    timer.value = window.setInterval(fn, delay)
  }

  onBeforeUnmount(() => stop())

  return { start, stop }
}

