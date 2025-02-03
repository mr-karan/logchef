import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import PrimeVue from 'primevue/config'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'
import './assets/style.css'

const app = createApp(App)

app.use(PrimeVue, {
    theme: {
        preset: Aura,
        options: {
            prefix: 'p',
            darkModeSelector: 'none', // Disable dark mode completely
            cssLayer: {
                name: 'primevue',
                order: 'reset, primevue' // Ensure PrimeVue styles take precedence over resets
            }
        }
    }
})

app.use(router)
app.mount('#app')
