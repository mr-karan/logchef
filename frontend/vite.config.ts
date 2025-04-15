import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import autoprefixer from "autoprefixer";
import tailwind from "tailwindcss";
import { defineConfig, loadEnv } from "vite";
import { resolve } from "path";
import compression from 'vite-plugin-compression';

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
    plugins: [
      vue(),
      compression({
        algorithm: 'brotliCompress',
        ext: '.br',
        threshold: 10240 // 10kb
      }),
      compression({
        algorithm: 'gzip',
        ext: '.gz',
        threshold: 10240
      })
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
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (id.includes('node_modules')) {
              // Split vendor chunks
              if (id.includes('vue')) {
                return 'vendor-vue';
              }
              if (id.includes('date-fns')) {
                return 'vendor-date-fns';
              }
              return 'vendor'; // Other node_modules
            }
          },
          // Better chunk naming
          chunkFileNames: 'assets/[name]-[hash].js',
          entryFileNames: 'assets/[name]-[hash].js',
          assetFileNames: 'assets/[name]-[hash][extname]'
        }
      },
      // Minification options
      minify: 'terser',
      terserOptions: {
        compress: {
          drop_console: true,
          drop_debugger: true
        }
      },
      chunkSizeWarningLimit: 1500 // Set higher warning limit
    },
    optimizeDeps: {
      include: [
        'monaco-editor',
        'vue',
        'vue-router',
        // Add other heavy dependencies
      ],
      exclude: ['vue-demi']
    },

    // Add these experimental options
    experimental: {
      renderBuiltUrl(filename) {
        return { relative: true };
      }
    }
  };
});
