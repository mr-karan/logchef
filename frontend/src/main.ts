import { createApp } from "vue";
import { createPinia } from "pinia";
import "./assets/index.css";
import App from "./App.vue";
import router from "./router";
import { useAuthStore } from "@/stores/auth";

async function initializeApp() {
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

  // Mount app only after auth is initialized
  app.mount("#app");
}

// Start the app
initializeApp().catch((error) => {
  console.error("Failed to initialize app:", error);
});
