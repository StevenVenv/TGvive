import { ref, watch } from 'vue'

export type UITheme = 'light' | 'dark'

const storageThemeKey = 'tgvive_ui_theme'

function getStoredTheme(): UITheme | null {
  try {
    const raw = (localStorage.getItem(storageThemeKey) || '').trim()
    if (raw === 'light' || raw === 'dark') return raw
  } catch {
    // ignore
  }
  return null
}

function prefersDark(): boolean {
  try {
    return !!window.matchMedia?.('(prefers-color-scheme: dark)')?.matches
  } catch {
    return false
  }
}

function applyTheme(v: UITheme) {
  try {
    const root = document.documentElement
    root.classList.toggle('dark', v === 'dark')
    root.style.colorScheme = v
  } catch {
    // ignore
  }
}

export function useTheme() {
  const theme = ref<UITheme>(getStoredTheme() ?? (prefersDark() ? 'dark' : 'light'))

  applyTheme(theme.value)
  watch(
    theme,
    (v) => {
      try {
        localStorage.setItem(storageThemeKey, v)
      } catch {
        // ignore
      }
      applyTheme(v)
    },
    { flush: 'post' },
  )

  function setTheme(v: UITheme) {
    theme.value = v
  }

  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  }

  return { theme, setTheme, toggleTheme }
}

