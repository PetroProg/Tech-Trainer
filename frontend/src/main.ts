import { createApp } from 'vue'
import './style.css'
import 'prismjs/themes/prism-tomorrow.css'
import App from './App.vue'
import { registerSW } from 'virtual:pwa-register'

registerSW({ immediate: true })

createApp(App).mount('#app')
