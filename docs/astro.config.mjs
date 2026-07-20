import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import sitemap from "@astrojs/sitemap";
import { passthroughImageService } from "astro/config";
import starlightLlmsTxt from "starlight-llms-txt";

const siteUrl = "https://logchef.app";
const ogImage = `${siteUrl}/screenshots/hero-light.png`;
const siteDescription =
  "Logchef is a lightweight, self-hosted log analytics and observability platform for teams that want a strong query and control plane on top of existing log backends (ClickHouse and VictoriaLogs). Query with LogchefQL, native SQL, or LogsQL, build dashboards, set up alerting, and manage access, all from a single binary.";

// https://astro.build/config
export default defineConfig({
  site: siteUrl,
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
      description: siteDescription,
      customCss: ["./src/assets/custom.css"],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/mr-karan/logchef",
        },
      ],
      // Header override: adds a "Blog" link next to the social icons in the
      // top-right of the docs header (keeps it out of the sidebar).
      components: {
        SocialIcons: "./src/components/starlight/SocialIcons.astro",
      },
      // "<Page title> | Logchef" for all docs pages.
      titleDelimiter: "|",
      // Show Starlight's built-in "last updated" date (derived from git history) on docs pages.
      lastUpdated: true,
      plugins: [
        // Generates /llms.txt + /llms-full.txt from the docs content at build time.
        starlightLlmsTxt({
          description: siteDescription,
          details: `Logchef supports two datasource backends with different query languages:

- **ClickHouse** sources: query with LogchefQL (Logchef's own simplified filter syntax) or native ClickHouse SQL.
- **VictoriaLogs** sources: query with LogsQL.

Key areas of the docs:

- Getting Started: install, configure, and understand the architecture.
- Querying Logs: LogchefQL syntax, SQL examples, the query interface, and the field sidebar.
- Features: collections/saved queries, dashboards, alerting, AI SQL generation, user management, service tokens, and declarative provisioning.
- Integration: CLI, MCP server (for AI assistants), and schema design guidance.
- Tutorials: connecting VictoriaLogs, ingesting via Vector/OTEL, and a worked NGINX logs example.
- Operations: database backends (SQLite vs Postgres) and the Prometheus metrics reference.`,
          optionalLinks: [
            {
              label: "GitHub repository",
              url: "https://github.com/mr-karan/logchef",
              description: "Source code, issues, and releases.",
            },
          ],
        }),
      ],
      // Starlight already emits per-page og:title, og:type, og:url, og:locale,
      // og:description, og:site_name, twitter:card (summary_large_image), and a
      // canonical <link> (since `site` is set above) — see
      // node_modules/@astrojs/starlight/utils/head.ts. Only add what Starlight
      // doesn't already provide: a shared OG/Twitter preview image, plus static
      // JSON-LD describing the site/organization.
      head: [
        // RSS autodiscovery for the blog feed (generated at /rss.xml).
        {
          tag: "link",
          attrs: {
            rel: "alternate",
            type: "application/rss+xml",
            title: "Logchef Blog",
            href: "/rss.xml",
          },
        },
        // OG image (Starlight has no default og:image).
        {
          tag: "meta",
          attrs: { property: "og:image", content: ogImage },
        },
        {
          tag: "meta",
          attrs: { property: "og:image:width", content: "2400" },
        },
        {
          tag: "meta",
          attrs: { property: "og:image:height", content: "1498" },
        },
        {
          tag: "meta",
          attrs: { property: "og:image:type", content: "image/png" },
        },
        {
          tag: "meta",
          attrs: { property: "og:image:alt", content: "Logchef log explorer" },
        },
        // Twitter card image (twitter:card is already set by Starlight).
        {
          tag: "meta",
          attrs: { name: "twitter:image", content: ogImage },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:image:alt", content: "Logchef log explorer" },
        },
        // Sitewide JSON-LD (WebSite + Organization). Per-page structured data
        // (e.g. Article/BreadcrumbList) is left to whoever owns BaseLayout.astro.
        {
          tag: "script",
          attrs: { type: "application/ld+json" },
          content: JSON.stringify({
            "@context": "https://schema.org",
            "@graph": [
              {
                "@type": "WebSite",
                "@id": `${siteUrl}/#website`,
                url: `${siteUrl}/`,
                name: "Logchef",
                description: siteDescription,
                publisher: { "@id": `${siteUrl}/#organization` },
              },
              {
                "@type": "Organization",
                "@id": `${siteUrl}/#organization`,
                name: "Logchef",
                url: `${siteUrl}/`,
                logo: {
                  "@type": "ImageObject",
                  url: `${siteUrl}/logo.svg`,
                },
                sameAs: ["https://github.com/mr-karan/logchef"],
              },
            ],
          }),
        },
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
            { label: "LogsQL Queries", link: "/guide/logsql-queries" },
            { label: "Field Sidebar", link: "/features/field-sidebar" },
          ],
        },
        {
          label: "Features",
          items: [
            { label: "Collections & Saved Queries", link: "/features/collections" },
            { label: "Dashboards", link: "/features/dashboards" },
            { label: "Alerting", link: "/features/alerting" },
            { label: "AI SQL Generation", link: "/features/ai-sql-generation" },
            { label: "User Management", link: "/core/user-management" },
            { label: "Service Tokens", link: "/features/service-tokens" },
            { label: "Provisioning", link: "/getting-started/provisioning" },
          ],
        },
        {
          label: "Integration",
          items: [
            { label: "Overview", link: "/integration" },
            { label: "Vector", link: "/integration/vector" },
            { label: "OpenTelemetry Collector", link: "/integration/otel-collector" },
            { label: "Kubernetes Logs", link: "/integration/kubernetes" },
            { label: "Docker Logs", link: "/integration/docker" },
            { label: "CLI", link: "/integration/cli" },
            { label: "MCP Server", link: "/integration/mcp-server" },
            { label: "Schema Design", link: "/integration/schema-design" },
          ],
        },
        {
          label: "Tutorials",
          items: [
            { label: "VictoriaLogs", link: "/tutorials/victorialogs" },
            { label: "VictoriaLogs Explorer", link: "/tutorials/victorialogs-explorer" },
            { label: "NGINX Logs", link: "/tutorials/nginx-logs" },
          ],
        },
        {
          label: "Comparisons",
          items: [
            { label: "Overview", link: "/comparisons" },
            { label: "Logchef vs ClickStack", link: "/comparisons/logchef-vs-clickstack" },
            { label: "Logchef vs Grafana Loki", link: "/comparisons/logchef-vs-grafana-loki" },
          ],
        },
        {
          label: "Migrate to Logchef",
          items: [
            { label: "Overview", link: "/migrate" },
            { label: "From Elasticsearch", link: "/migrate/from-elasticsearch" },
            { label: "From Loki", link: "/migrate/from-loki" },
          ],
        },
        {
          label: "Operations",
          items: [
            { label: "Database & High Availability", link: "/operations/database-backends" },
            { label: "Metrics Reference", link: "/operations/metrics" },
            { label: "Contributing", link: "/contributing/setup" },
          ],
        },
      ],
    }),
    // Emits sitemap-index.xml + sitemap-0.xml at build time (needs `site`,
    // set above). robots.txt points crawlers at /sitemap-index.xml.
    sitemap(),
  ],
});
