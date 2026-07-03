import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    include: ['src/**/__tests__/**/*.test.{ts,tsx}'],
    environment: 'node',
    globals: true,
    // Polyfill browser globals that some renderer modules touch at import time
    // (localStorage in particular). Component tests opt into a full DOM via the
    // `// @vitest-environment jsdom` docblock; this setup only backfills the
    // storage shim jsdom does not provide a working implementation for here.
    setupFiles: ['src/test/setup-globals.ts'],
  },
})
