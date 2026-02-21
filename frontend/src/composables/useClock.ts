import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function fmtHHMMSS(ts: number): string {
  const d = new Date(ts || Date.now())
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

export function useClock(intervalMs = 1000) {
  const now = ref(Date.now())
  let timer: number | undefined

  onMounted(() => {
    timer = window.setInterval(() => {
      now.value = Date.now()
    }, Math.max(250, Math.floor(intervalMs)))
  })

  onBeforeUnmount(() => {
    if (timer) window.clearInterval(timer)
    timer = undefined
  })

  const timeText = computed(() => fmtHHMMSS(now.value))
  const dateText = computed(() => {
    try {
      return new Date(now.value).toLocaleDateString()
    } catch {
      return ''
    }
  })

  return { now, timeText, dateText }
}

