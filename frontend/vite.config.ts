import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import autoprefixer from "autoprefixer";
import tailwind from "tailwindcss";
import { defineConfig, loadEnv, type Plugin, type UserConfig } from "rolldown-vite";
import { resolve } from "path";

// https://vite.dev/config/
export default defineConfig(async ({ mode }): Promise<UserConfig> => {
  // Load env file based on `mode` in the current working directory.
  // Set the third parameter to '' to load all env regardless of the `VITE_` prefix.
  const env = loadEnv(mode, process.cwd(), "");

  const apiUrl = env.VITE_API_URL || "http://localhost:8125";
  const isAnalyze = mode === "analyze";

  // Conditionally load visualizer only when analyzing
  const plugins: Plugin[] = [vue()];

  if (isAnalyze) {
    // Dynamic import for visualizer - only loaded when needed
    const { visualizer } = await import("rollup-plugin-visualizer");
    plugins.push(
      visualizer({
        template: "treemap",
        open: true,
        gzipSize: true,
        brotliSize: true,
        filename: "stats.html",
      })
    );
  }

  return {
    css: {
      postcss: {
        plugins: [tailwind(), autoprefixer()],
      },
      devSourcemap: false,
    },
    plugins,
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
      sourcemap: false,
      chunkSizeWarningLimit: 1000,
      // Use esbuild for minification (10-20x faster than terser)
      minify: "esbuild",
      // esbuild minification options
      target: "es2020",
      cssMinify: "esbuild",
      rollupOptions: {
        output: {
          // Improved chunk splitting strategy
          manualChunks: (id: string) => {
            // Monaco editor - largest dependency, separate chunk
            if (id.includes("monaco-editor")) {
              return "monaco-editor";
            }
            // ECharts - tree-shaken but still substantial
            if (id.includes("echarts")) {
              return "echarts";
            }
            // Vue ecosystem - changes less frequently
            if (id.includes("node_modules/vue") || 
                id.includes("node_modules/@vue") ||
                id.includes("node_modules/pinia") ||
                id.includes("node_modules/vue-router")) {
              return "vue-vendor";
            }
            // UI libraries - radix, reka, etc.
            if (id.includes("radix-vue") || 
                id.includes("reka-ui") ||
                id.includes("vaul-vue")) {
              return "ui-vendor";
            }
            // Date utilities
            if (id.includes("date-fns") || id.includes("@internationalized/date")) {
              return "date-utils";
            }
            // Other vendor libs (including node-sql-parser with its lodash dependency)
            if (id.includes("node_modules")) {
              return "vendor";
            }
          },
          entryFileNames: "assets/[name]-[hash].js",
          chunkFileNames: "assets/[name]-[hash].js",
          assetFileNames: "assets/[name]-[hash][extname]",
        },
      },
      // Enable build caching for faster rebuilds
      reportCompressedSize: false, // Skip gzip size calculation (faster builds)
    },
    // Optimize dependency pre-bundling
    optimizeDeps: {
      include: [
        "monaco-editor",
        "vue",
        "vue-router",
        "pinia",
        "lodash-es",
        "@vueuse/core",
      ],
      exclude: ["@guolao/vue-monaco-editor"], // Let it use the pre-bundled monaco
    },
    // Enable caching
    cacheDir: "node_modules/.vite",
    esbuild: {
      // Drop console.log in production (equivalent to terser's drop_console)
      drop: mode === "production" ? ["console", "debugger"] : [],
      // Faster parsing
      legalComments: "none",
    },
  };
});