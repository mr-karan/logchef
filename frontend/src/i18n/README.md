# Frontend translations

Logchef uses Vue I18n 11 in Composition API mode (`legacy: false`). JSON catalogs
live in `locales/`. English is bundled as the fallback; other catalogs load on
demand. This keeps language packs out of the initial application download.
The Vite build disables the unused legacy API using Vue I18n's
[build flags](https://vue-i18n.intlify.dev/guide/advanced/optimization#feature-build-flags).

Settings → Preferences → Language saves the selected language to the user's
account. Browser language is the default. Public read-only demos save preferences
in the browser instead. Language changes do not reload the page.

## Writing messages

Use `useI18n()` inside component setup or a composable called from setup:

```ts
const { t } = useI18n();
const heading = computed(() => t("ui.preferences"));
```

Translate complete messages, not sentence fragments. Pass names and counts as
named parameters. Use `i18n-t` with named slots for messages containing links or
code. Never put HTML in a catalog or render translations with `v-html`.

Do not translate stored log data, field names, SQL/LogchefQL/LogsQL keywords,
query examples, backend error details, or user-created names and descriptions.
Translate interface labels around these values instead. Keep stored enum values
unchanged. Build labels in computed values, getters, or the render path so they
respond to language changes.

## Adding a language

1. Copy `locales/en.json` to a file named with the language's BCP 47 tag.
2. Translate every value. Keep keys and interpolation parameter names unchanged.
   Use Vue I18n plural forms where the language requires them.
3. Register its code and native name in `locales.ts`. Add browser-tag matching
   only if normal base-language matching is insufficient.
4. Add a `LocalePreference` constant in `pkg/models/preferences.go`, then add it
   to `isValidLocalePreference` in `internal/core/user_preferences.go` and its tests.
5. Run `bun run test src/i18n`, `bun run typecheck`, and `bun run build` from
   `frontend/`. Test Settings, Explorer, Dashboards, and Alerts in the browser.

The catalog tests check language registration, key parity, nonempty messages,
interpolation parameters, message compilation, and literal keys used by the UI.
They run in the existing frontend CI workflow. English fallback still handles
missing messages at runtime, but incomplete catalogs fail CI.

The initial non-English catalogs are translation drafts. Native speakers should
review terminology and phrasing before they are described as reviewed translations.
Right-to-left languages need a separate layout review before being advertised.
