import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { passthroughImageService } from "astro/config";

// https://astro.build/config
export default defineConfig({
  site: "https://logchef.app",
  image: {
    service: passthroughImageService(),
  },
  integrations: [
    starlight({
      title: "Logchef",
      logo: {
        src: "./public/logo.svg",
        alt: "Logchef",
      },
      description: "Log analytics platform for ClickHouse and VictoriaLogs",
      customCss: ["./src/assets/custom.css"],
      social: {
        github: "https://github.com/mr-karan/logchef",
      },
      head: [
        // Umami Analytics
        {
          tag: "script",
          attrs: {
            defer: true,
            src: "https://um.mrkaran.dev/script.js",
            "data-website-id": "7c608903-19f6-4782-8def-c03e71ff35fc",
          },
        },
        // Sync theme with standalone pages
        {
          tag: "script",
          content: `
            (function() {
              const theme = localStorage.getItem('logchef-theme');
              if (theme === 'light') {
                document.documentElement.dataset.theme = 'light';
              } else if (theme === 'dark') {
                document.documentElement.dataset.theme = 'dark';
              }
            })();
          `,
        },
      ],
      sidebar: [
        {
          label: "Getting Started",
          items: [
            { label: "Quick Start", link: "/getting-started/quickstart" },
            { label: "Configuration", link: "/getting-started/configuration" },
            { label: "Architecture", link: "/core/architecture" },
          ],
        },
        {
          label: "Querying Logs",
          items: [
            { label: "Query Interface", link: "/user-guide/query-interface" },
            { label: "LogchefQL Syntax", link: "/guide/search-syntax" },
            { label: "SQL Examples", link: "/guide/examples" },
            { label: "Field Sidebar", link: "/features/field-sidebar" },
          ],
        },
        {
          label: "Features",
          items: [
            { label: "Alerting", link: "/features/alerting" },
            { label: "AI SQL Generation", link: "/features/ai-sql-generation" },
            { label: "User Management", link: "/core/user-management" },
            { label: "Provisioning", link: "/getting-started/provisioning" },
          ],
        },
        {
          label: "Integration",
          items: [
            { label: "CLI", link: "/integration/cli" },
            { label: "MCP Server", link: "/integration/mcp-server" },
            { label: "Schema Design", link: "/integration/schema-design" },
          ],
        },
        {
          label: "Tutorials",
          items: [
            { label: "Vector + OTEL Logs", link: "/tutorials/vector-otel" },
            { label: "NGINX Logs", link: "/tutorials/nginx-logs" },
          ],
        },
        {
          label: "Operations",
          items: [
            { label: "Metrics Reference", link: "/operations/metrics" },
            { label: "Contributing", link: "/contributing/setup" },
          ],
        },
      ],
    }),
  ],
});
