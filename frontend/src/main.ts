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

createApp(App).mount('#app')
