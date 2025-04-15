import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import autoprefixer from "autoprefixer";
import tailwind from "tailwindcss";
import { defineConfig, loadEnv } from "vite";
import { resolve } from "path";

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Load env file based on `mode` in the current working directory.
  // Set the third parameter to '' to load all env regardless of the `VITE_` prefix.
  const env = loadEnv(mode, process.cwd(), "");

  const apiUrl = env.VITE_API_URL || "http://localhost:8125";

  return {
    css: {
      postcss: {
        plugins: [tailwind(), autoprefixer()],
      },
    },
    plugins: [vue()],
    resolve: {
      alias: {
        "@": fileURLToPath(new URL("./src", import.meta.url)),
      },
    },
    server: {
      proxy: {
        "/api": {
          target: apiUrl,
          changeOrigin: true,
          secure: false,
        },
      },
    },
    build: {
      outDir: resolve(__dirname, "../cmd/server/ui"),
      emptyOutDir: true,
      rollupOptions: {
        output: {
          manualChunks: {
            // Create a separate chunk for Monaco Editor
            // This helps with lazy loading and reduces the initial bundle size
            "monaco-editor": ["monaco-editor"],
          },
        },
      },
    },
    optimizeDeps: {
      include: ["monaco-editor"],
    },
  };
});