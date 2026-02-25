import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import 'remixicon/fonts/remixicon.css'
import './style.css'
import './styles/admin.scss'
import App from './App.vue'
import { ensureBackendOrigin } from './runtime/backend'

await ensureBackendOrigin()

createApp(App).use(ElementPlus).mount('#app')
