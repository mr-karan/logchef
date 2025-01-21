import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

import tailwind from 'tailwindcss'
import autoprefixer from 'autoprefixer'

// Export a function to use dynamic configurations based on the environment
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const API_URL = env.API_URL || 'http://localhost:8125'

  console.log('API URL:', API_URL) // This will show you what URL is being loaded

  return {
    css: {
      postcss: {
        plugins: [tailwind(), autoprefixer()]
      }
    },
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      proxy: {
        '/api': {
          target: API_URL
          // changeOrigin: true,
          // secure: false,
          // rewrite: path => path.replace(/^\/api/, '')
        }
      }
    },
    define: {
      __APP_ENV__: JSON.stringify(env.APP_ENV)
    },
    base: '/ui/',
    build: {
      outDir: '../pkg/ui/dist',
      emptyOutDir: true,
    }
  }
})
