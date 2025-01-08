import { fileURLToPath, URL } from 'node:url';
import { loadEnv } from 'vite';
import vue from '@vitejs/plugin-vue';
import autoprefixer from 'autoprefixer';
import tailwind from 'tailwindcss';
import { defineConfig } from 'vite';
// https://vite.dev/config/
export default defineConfig(function (_a) {
    var mode = _a.mode;
    // Load env file based on `mode` in the current working directory.
    // Set the third parameter to '' to load all env regardless of the `VITE_` prefix.
    var env = loadEnv(mode, process.cwd(), '');
    return {
        css: {
            postcss: {
                plugins: [tailwind(), autoprefixer()],
            },
        },
        plugins: [vue()],
        resolve: {
            alias: [
                {
                    find: '@',
                    replacement: fileURLToPath(new URL('./src', import.meta.url))
                }
            ]
        },
        server: {
            proxy: {
                '/api': {
                    target: env.VITE_API_BASE_URL || 'http://localhost:8125',
                    changeOrigin: true,
                    secure: false
                }
            }
        },
        build: {
            outDir: '../backend/cmd/server/ui',
            emptyOutDir: true,
        },
    };
});
