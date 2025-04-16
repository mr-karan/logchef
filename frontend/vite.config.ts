import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import autoprefixer from "autoprefixer";
import tailwind from "tailwindcss";
import { defineConfig, loadEnv } from "vite";
import { resolve } from "path";
import { visualizer } from "rollup-plugin-visualizer";

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
      devSourcemap: false, // Disable CSS source maps in production
    },
    plugins: [vue(),
      visualizer({
        template: "treemap", // Show treemap view
        open: true,
        gzipSize: true,
        brotliSize: true,
      }) // Add bundle analyzer
    ],
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
      sourcemap: false, // Disable source maps
      chunkSizeWarningLimit: 1000, // Increase chunk size warning limit (in kB)
      rollupOptions: {
        output: {
          manualChunks: {
            // Create a separate chunk for Monaco Editor
            // This helps with lazy loading and reduces the initial bundle size
            "monaco-editor": ["monaco-editor"],
          },
          entryFileNames: "assets/[name]-[hash].js",
          chunkFileNames: "assets/[name]-[hash].js",
          assetFileNames: "assets/[name]-[hash][extname]",
        },
      },
      minify: "terser",
      terserOptions: {
        compress: {
          drop_console: true, // Remove console logs in production
        },
      },
    },
    optimizeDeps: {
      include: ["monaco-editor"],
    },
  };
});