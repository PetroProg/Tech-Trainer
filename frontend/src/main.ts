import { createApp } from 'vue'
import './style.css'
import 'prismjs/themes/prism-tomorrow.css'
import App from './App.vue'
import { registerSW } from 'virtual:pwa-register'

// Automatic PWA service worker updater:
// When the app is re-opened or resumed, it immediately checks for new versions.
// If an update is ready, it applies it instantly and refreshes without needing a browser!
const updateSW = registerSW({
  immediate: true,
  onNeedRefresh() {
    updateSW(true)
  },
  onRegisteredSW(_swUrl, registration) {
    if (registration) {
      // Check for update whenever user returns to the app
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') {
          registration.update().catch(() => {})
        }
      })
      // Periodic check every 10 minutes
      setInterval(() => {
        registration.update().catch(() => {})
      }, 10 * 60 * 1000)
    }
  }
})

// Auto-reload on service worker transition
if ('serviceWorker' in navigator) {
  let refreshing = false
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (!refreshing) {
      refreshing = true
      window.location.reload()
    }
  })
}

const app = createApp(App)

app.config.errorHandler = (err: unknown, _instance, info) => {
  console.error('[TechTrainer Error]', err, info)
}

window.addEventListener('error', (event) => {
  console.error('[TechTrainer Global Error]', event.error || event.message)
  const appEl = document.getElementById('app')
  if (appEl && appEl.children.length === 0) {
    appEl.innerHTML = `
      <div style="min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 24px; text-align: center; font-family: sans-serif; color: #f1f5f9; background-color: #0f172a;">
        <div style="font-size: 20px; font-weight: bold; margin-bottom: 12px; color: #38bdf8;">Tech Trainer</div>
        <div style="font-size: 14px; color: #94a3b8; max-width: 400px; margin-bottom: 20px; line-height: 1.5;">An error occurred while loading the application. Click below to refresh.</div>
        <button onclick="window.location.reload(true)" style="background: #0284c7; color: white; border: none; padding: 10px 20px; border-radius: 12px; font-weight: 600; cursor: pointer;">Refresh App</button>
      </div>
    `
  }
})

app.mount('#app')
