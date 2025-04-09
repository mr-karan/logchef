import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { passthroughImageService } from "astro/config";

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
          label: "Introduction",
          items: [
            { label: "Quick Start", link: "/getting-started/quickstart" },
            { label: "Architecture", link: "/core/architecture" },
            { label: "Data Model", link: "/core/data-model" },
          ],
        },
        {
          label: "Setup & Configuration",
          items: [
            { label: "Configuration Guide", link: "/getting-started/configuration" },
            { label: "Vector Setup", link: "/integration/vector" },
            { label: "Schema Design", link: "/integration/schema-design" },
          ],
        },
        {
          label: "Using LogChef",
          items: [
            { label: "Search Guide", link: "/guide/search-syntax" },
            { label: "Query Examples", link: "/guide/examples" },
            { label: "User Management", link: "/core/user-management" },
          ],
        },
        {
          label: "Project",
          items: [
            { label: "Contributing", link: "/contributing/setup" },
            { label: "Roadmap", link: "/contributing/roadmap" },
          ],
        },
      ],
    }),
  ],
});
