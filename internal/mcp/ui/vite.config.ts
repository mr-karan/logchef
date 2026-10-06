import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    cssCodeSplit: false,
    assetsInlineLimit: 1000000,
    rolldownOptions: { output: { codeSplitting: false } },
  },
})
