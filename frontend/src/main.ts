import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import 'remixicon/fonts/remixicon.css'
import './style.css'
import './styles/admin.scss'
import App from './App.vue'

const tokenStorageKey = 'tgvive_jwt_token'

async function bootstrapDevToken() {
  try {
    const existing = (localStorage.getItem(tokenStorageKey) || '').trim()
    if (existing) return
  } catch {
    return
  }

  try {
    const res = await fetch('/api/v1/auth/dev/token?username=admin', { method: 'GET' })
    const json = (await res.json()) as any
    const token = String(json?.data?.token || '').trim()
    if (json?.code === 0 && token) {
      localStorage.setItem(tokenStorageKey, token)
    }
  } catch {
    // ignore (likely non-debug mode)
  }
}

await bootstrapDevToken()

createApp(App).use(ElementPlus).mount('#app')
