import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { defineConfig, passthroughImageService } from "astro/config";

// https://astro.build/config
export default defineConfig({
  base: "/docs",
  site: "https://logchef.mrkaran.dev/docs/",
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
          label: "Getting Started",
          items: [
            { label: "Quick Start", link: "/getting-started/quickstart" },
            { label: "Installation", link: "/getting-started/installation" },
            { label: "Configuration", link: "/getting-started/configuration" },
          ],
        },
        {
          label: "Core Concepts",
          items: [
            { label: "Architecture", link: "/core/architecture" },
            { label: "Architecture Diagram", link: "/core/architecture-diagram" },
            { label: "Data Model", link: "/core/data-model" },
            { label: "Query Interface", link: "/core/query-interface" },
            { label: "User Management", link: "/core/user-management" },
          ],
        },
        {
          label: "User Guide",
          items: [
            { label: "Query Examples", link: "/guide/examples" },
            { label: "Search Syntax", link: "/guide/search-syntax" },
            { label: "Visualizations", link: "/guide/visualizations" },
            { label: "Alerts", link: "/guide/alerts" },
          ],
        },
        {
          label: "Integration",
          items: [
            { label: "Vector Setup", link: "/integration/vector" },
            { label: "Schema Design", link: "/integration/schema-design" },
            { label: "HTTP API", link: "/integration/api" },
            { label: "Client Libraries", link: "/integration/clients" },
          ],
        },
        {
          label: "Administration",
          items: [
            { label: "Deployment", link: "/admin/deployment" },
            { label: "Security", link: "/admin/security" },
            { label: "Performance", link: "/admin/performance" },
            { label: "Backup & Recovery", link: "/admin/backup" },
            { label: "Monitoring", link: "/admin/monitoring" },
          ],
        },
        {
          label: "Advanced",
          items: [
            { label: "SQL Reference", link: "/advanced/sql-reference" },
            { label: "Query Optimization", link: "/advanced/query-optimization" },
            { label: "Custom Functions", link: "/advanced/custom-functions" },
            { label: "Materialized Views", link: "/advanced/materialized-views" },
          ],
        },
        {
          label: "Contributing",
          items: [
            { label: "Development Setup", link: "/contributing/setup" },
            { label: "Code Structure", link: "/contributing/codebase" },
            { label: "Testing", link: "/contributing/testing" },
            { label: "Documentation", link: "/contributing/docs" },
            { label: "Roadmap", link: "/contributing/roadmap" },
          ],
        },
      ],
    }),
  ],
});
