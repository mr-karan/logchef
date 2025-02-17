import { createApp } from "vue";
import { createPinia } from "pinia";
import "./assets/index.css";
import App from "./App.vue";
import router from "./router";
import { useAuthStore } from "@/stores/auth";

// Create app instance
const app = createApp(App);

// Create and use Pinia before router
const pinia = createPinia();
app.use(pinia);

// Use router after Pinia
app.use(router);

// Mount app
app.mount("#app");
