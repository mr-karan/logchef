// vite.config.js
import { defineConfig, loadEnv } from "file:///home/karan/Code/Personal/logchef/ui/node_modules/vite/dist/node/index.js";
import vue from "file:///home/karan/Code/Personal/logchef/ui/node_modules/@vitejs/plugin-vue/dist/index.mjs";
import { fileURLToPath, URL } from "node:url";
import tailwind from "file:///home/karan/Code/Personal/logchef/ui/node_modules/tailwindcss/lib/index.js";
import autoprefixer from "file:///home/karan/Code/Personal/logchef/ui/node_modules/autoprefixer/lib/autoprefixer.js";
var __vite_injected_original_import_meta_url = "file:///home/karan/Code/Personal/logchef/ui/vite.config.js";
var vite_config_default = defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const API_URL = env.API_URL || "http://localhost:8125";
  console.log("API URL:", API_URL);
  return {
    css: {
      postcss: {
        plugins: [tailwind(), autoprefixer()]
      }
    },
    plugins: [vue()],
    resolve: {
      alias: {
        "@": fileURLToPath(new URL("./src", __vite_injected_original_import_meta_url))
      }
    },
    server: {
      proxy: {
        "/api": {
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
    base: "/ui/",
    build: {
      outDir: "../pkg/ui/dist",
      emptyOutDir: true
    }
  };
});
export {
  vite_config_default as default
};
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsidml0ZS5jb25maWcuanMiXSwKICAic291cmNlc0NvbnRlbnQiOiBbImNvbnN0IF9fdml0ZV9pbmplY3RlZF9vcmlnaW5hbF9kaXJuYW1lID0gXCIvaG9tZS9rYXJhbi9Db2RlL1BlcnNvbmFsL2xvZ2NoZWYvdWlcIjtjb25zdCBfX3ZpdGVfaW5qZWN0ZWRfb3JpZ2luYWxfZmlsZW5hbWUgPSBcIi9ob21lL2thcmFuL0NvZGUvUGVyc29uYWwvbG9nY2hlZi91aS92aXRlLmNvbmZpZy5qc1wiO2NvbnN0IF9fdml0ZV9pbmplY3RlZF9vcmlnaW5hbF9pbXBvcnRfbWV0YV91cmwgPSBcImZpbGU6Ly8vaG9tZS9rYXJhbi9Db2RlL1BlcnNvbmFsL2xvZ2NoZWYvdWkvdml0ZS5jb25maWcuanNcIjtpbXBvcnQgeyBkZWZpbmVDb25maWcsIGxvYWRFbnYgfSBmcm9tICd2aXRlJ1xuaW1wb3J0IHZ1ZSBmcm9tICdAdml0ZWpzL3BsdWdpbi12dWUnXG5pbXBvcnQgeyBmaWxlVVJMVG9QYXRoLCBVUkwgfSBmcm9tICdub2RlOnVybCdcblxuaW1wb3J0IHRhaWx3aW5kIGZyb20gJ3RhaWx3aW5kY3NzJ1xuaW1wb3J0IGF1dG9wcmVmaXhlciBmcm9tICdhdXRvcHJlZml4ZXInXG5cbi8vIEV4cG9ydCBhIGZ1bmN0aW9uIHRvIHVzZSBkeW5hbWljIGNvbmZpZ3VyYXRpb25zIGJhc2VkIG9uIHRoZSBlbnZpcm9ubWVudFxuZXhwb3J0IGRlZmF1bHQgZGVmaW5lQ29uZmlnKCh7IG1vZGUgfSkgPT4ge1xuICBjb25zdCBlbnYgPSBsb2FkRW52KG1vZGUsIHByb2Nlc3MuY3dkKCksICcnKVxuICBjb25zdCBBUElfVVJMID0gZW52LkFQSV9VUkwgfHwgJ2h0dHA6Ly9sb2NhbGhvc3Q6ODEyNSdcblxuICBjb25zb2xlLmxvZygnQVBJIFVSTDonLCBBUElfVVJMKSAvLyBUaGlzIHdpbGwgc2hvdyB5b3Ugd2hhdCBVUkwgaXMgYmVpbmcgbG9hZGVkXG5cbiAgcmV0dXJuIHtcbiAgICBjc3M6IHtcbiAgICAgIHBvc3Rjc3M6IHtcbiAgICAgICAgcGx1Z2luczogW3RhaWx3aW5kKCksIGF1dG9wcmVmaXhlcigpXVxuICAgICAgfVxuICAgIH0sXG4gICAgcGx1Z2luczogW3Z1ZSgpXSxcbiAgICByZXNvbHZlOiB7XG4gICAgICBhbGlhczoge1xuICAgICAgICAnQCc6IGZpbGVVUkxUb1BhdGgobmV3IFVSTCgnLi9zcmMnLCBpbXBvcnQubWV0YS51cmwpKVxuICAgICAgfVxuICAgIH0sXG4gICAgc2VydmVyOiB7XG4gICAgICBwcm94eToge1xuICAgICAgICAnL2FwaSc6IHtcbiAgICAgICAgICB0YXJnZXQ6IEFQSV9VUkxcbiAgICAgICAgICAvLyBjaGFuZ2VPcmlnaW46IHRydWUsXG4gICAgICAgICAgLy8gc2VjdXJlOiBmYWxzZSxcbiAgICAgICAgICAvLyByZXdyaXRlOiBwYXRoID0+IHBhdGgucmVwbGFjZSgvXlxcL2FwaS8sICcnKVxuICAgICAgICB9XG4gICAgICB9XG4gICAgfSxcbiAgICBkZWZpbmU6IHtcbiAgICAgIF9fQVBQX0VOVl9fOiBKU09OLnN0cmluZ2lmeShlbnYuQVBQX0VOVilcbiAgICB9LFxuICAgIGJhc2U6ICcvdWkvJyxcbiAgICBidWlsZDoge1xuICAgICAgb3V0RGlyOiAnLi4vcGtnL3VpL2Rpc3QnLFxuICAgICAgZW1wdHlPdXREaXI6IHRydWUsXG4gICAgfVxuICB9XG59KVxuIl0sCiAgIm1hcHBpbmdzIjogIjtBQUE4UixTQUFTLGNBQWMsZUFBZTtBQUNwVSxPQUFPLFNBQVM7QUFDaEIsU0FBUyxlQUFlLFdBQVc7QUFFbkMsT0FBTyxjQUFjO0FBQ3JCLE9BQU8sa0JBQWtCO0FBTHVKLElBQU0sMkNBQTJDO0FBUWpPLElBQU8sc0JBQVEsYUFBYSxDQUFDLEVBQUUsS0FBSyxNQUFNO0FBQ3hDLFFBQU0sTUFBTSxRQUFRLE1BQU0sUUFBUSxJQUFJLEdBQUcsRUFBRTtBQUMzQyxRQUFNLFVBQVUsSUFBSSxXQUFXO0FBRS9CLFVBQVEsSUFBSSxZQUFZLE9BQU87QUFFL0IsU0FBTztBQUFBLElBQ0wsS0FBSztBQUFBLE1BQ0gsU0FBUztBQUFBLFFBQ1AsU0FBUyxDQUFDLFNBQVMsR0FBRyxhQUFhLENBQUM7QUFBQSxNQUN0QztBQUFBLElBQ0Y7QUFBQSxJQUNBLFNBQVMsQ0FBQyxJQUFJLENBQUM7QUFBQSxJQUNmLFNBQVM7QUFBQSxNQUNQLE9BQU87QUFBQSxRQUNMLEtBQUssY0FBYyxJQUFJLElBQUksU0FBUyx3Q0FBZSxDQUFDO0FBQUEsTUFDdEQ7QUFBQSxJQUNGO0FBQUEsSUFDQSxRQUFRO0FBQUEsTUFDTixPQUFPO0FBQUEsUUFDTCxRQUFRO0FBQUEsVUFDTixRQUFRO0FBQUE7QUFBQTtBQUFBO0FBQUEsUUFJVjtBQUFBLE1BQ0Y7QUFBQSxJQUNGO0FBQUEsSUFDQSxRQUFRO0FBQUEsTUFDTixhQUFhLEtBQUssVUFBVSxJQUFJLE9BQU87QUFBQSxJQUN6QztBQUFBLElBQ0EsTUFBTTtBQUFBLElBQ04sT0FBTztBQUFBLE1BQ0wsUUFBUTtBQUFBLE1BQ1IsYUFBYTtBQUFBLElBQ2Y7QUFBQSxFQUNGO0FBQ0YsQ0FBQzsiLAogICJuYW1lcyI6IFtdCn0K
