/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Public, build-time-only credentials for purpose-built demo images. */
  readonly VITE_DEMO_LOGIN_EMAIL?: string
  readonly VITE_DEMO_LOGIN_PASSWORD?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
