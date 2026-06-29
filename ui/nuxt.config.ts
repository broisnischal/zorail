// Zorail dashboard — a client-side SPA (ssr: false). `nuxt generate` emits a
// static bundle to .output/public, which the Go server embeds and serves, so
// the whole product still ships as one binary / one Docker image.
export default defineNuxtConfig({
  compatibilityDate: '2025-06-01',
  ssr: false,
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],

  // Self-hosts fonts at build time (no runtime CDN), keeping the generated
  // output fully self-contained for air-gapped self-hosting.
  modules: ['@nuxt/fonts'],
  fonts: {
    families: [
      { name: 'Geist', provider: 'google', weights: [400, 500, 600, 700] },
      { name: 'Geist Mono', provider: 'google', weights: [400, 500, 600] },
    ],
  },

  app: {
    baseURL: '/',
    head: {
      title: 'zorail',
      meta: [
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'self-hosted disposable inboxes' },
      ],
      link: [
        {
          rel: 'icon',
          href:
            "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>📭</text></svg>",
        },
      ],
    },
  },

  runtimeConfig: {
    public: { apiBase: '/api' },
  },

  // In dev (nuxt on :3000), proxy API calls to the Go server on :8090 so the
  // frontend always talks to a same-origin /api path. In production the Go
  // server serves both the static UI and /api, so no proxy is needed.
  nitro: {
    devProxy: {
      '/api': { target: 'http://127.0.0.1:8090/api', changeOrigin: true },
    },
  },
})
