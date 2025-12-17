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
      description: "Modern, high-performance log analytics platform",
      customCss: ["./src/assets/custom.css"],
      social: {
        github: "https://github.com/mr-karan/logchef",
      },
      sidebar: [
        {
          label: "Introduction",
          items: [
            { label: "Quick Start", link: "/getting-started/quickstart" },
            { label: "Architecture", link: "/core/architecture" },
          ],
        },
        {
          label: "Setup & Configuration",
          items: [
            {
              label: "Configuration Guide",
              link: "/getting-started/configuration",
            },
          ],
        },
        {
          label: "Tutorials",
          items: [
            { label: "Shipping Logs with Vector (OTEL)", link: "/tutorials/vector-otel" },
            { label: "NGINX Logs in ClickHouse", link: "/tutorials/nginx-logs" },
          ],
        },
        {
          label: "Integration",
          items: [
            { label: "MCP Server", link: "/integration/mcp-server" },
            { label: "Schema Design", link: "/integration/schema-design" },
          ],
        },
        {
          label: "Using LogChef",
          items: [
            { label: "Query Interface", link: "/user-guide/query-interface" },
            { label: "Field Sidebar", link: "/features/field-sidebar" },
            { label: "AI SQL Generation", link: "/features/ai-sql-generation" },
            { label: "Search Guide", link: "/guide/search-syntax" },
            { label: "Query Examples", link: "/guide/examples" },
            { label: "User Management", link: "/core/user-management" },
          ],
        },
        {
          label: "Operations",
          items: [
            { label: "Metrics Reference", link: "/operations/metrics" },
          ],
        },
        // {
        //   label: "Project",
        //   items: [
        //     { label: "Roadmap", link: "/contributing/roadmap" },
        //   ],
        // },
      ],
    }),
  ],
});
