import { createApp } from "vue";
import { createPinia } from "pinia";
import "./assets/index.css";
import App from "./App.vue";
import router from "./router";
import { useAuthStore } from "@/stores/auth";
import { initMonacoSetup } from "@/utils/monaco";

async function initializeApp() {
  try {
    // Initialize Monaco editor setup
    initMonacoSetup();

    // Create app instance
    const app = createApp(App);

    // Add global error handler
    app.config.errorHandler = (err, instance, info) => {
      console.error('Vue Error:', err);
      console.error('Error Info:', info);
      console.error('Component:', instance);
    };

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
    
    // Display a minimal error message if app fails to initialize
    const rootEl = document.getElementById('app');
    if (rootEl) {
      rootEl.innerHTML = `
        <div style="padding: 20px; text-align: center;">
          <h2>Application Failed to Start</h2>
          <p>Please check the console for more details or try refreshing the page.</p>
          <button onclick="window.location.reload()" 
                  style="padding: 8px 16px; background: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer; margin-top: 20px;">
            Reload Application
          </button>
        </div>
      `;
    }
  }
}

// Start the app
initializeApp();
