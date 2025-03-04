import { createApp } from "vue";
import { createPinia } from "pinia";
import "./assets/index.css";
import App from "./App.vue";
import router from "./router";
import { useAuthStore } from "@/stores/auth";
import { initMonacoSetup, logMonacoInstanceCounts } from '@/utils/monaco';

// FUNDAMENTAL CHANGE: Initialize Monaco ONCE globally at app startup
// Monaco is a global singleton by design, trying to reinitialize causes issues
console.log("Initializing Monaco at app startup");
initMonacoSetup();

// Clean simple navigation logging
router.beforeEach((to, from, next) => {
  // Only log navigation between routes for debugging
  if (process.env.NODE_ENV !== 'production') {
    console.log(`Navigation from ${from.path} to ${to.path}`);
  }
  
  next();
});

async function initializeApp() {
  try {
    // Create app instance
    const app = createApp(App);

    // Create and use Pinia before router
    const pinia = createPinia();
    app.use(pinia);

    // Initialize auth store before router
    const authStore = useAuthStore(pinia);
    await authStore.initialize();

    // Use router after auth is initialized
    app.use(router);

    // Mount app
    app.mount("#app");
  } catch (error) {
    console.error("Failed to initialize app:", error);
  }
}

// Start the app
initializeApp();
