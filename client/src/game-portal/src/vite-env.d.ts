/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Base URL for the Go API server, e.g. "http://localhost:8080" or "https://api.example.com".
   *  The WebSocket URL is derived from this by swapping http->ws / https->wss. */
  readonly VITE_API_BASE_URL: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
