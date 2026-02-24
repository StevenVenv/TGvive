import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import 'remixicon/fonts/remixicon.css'
import './style.css'
import './styles/admin.scss'
import App from './App.vue'

async function bootstrapDevToken() {
  const host = String(window?.location?.hostname || '').trim().toLowerCase()
  if (host !== 'localhost' && host !== '127.0.0.1' && host !== '::1') return
  try {
    await fetch('/api/v1/auth/dev/token?username=admin', { method: 'GET', credentials: 'include' })
  } catch {
    // ignore (likely non-debug mode)
  }
}

await bootstrapDevToken()

createApp(App).use(ElementPlus).mount('#app')
