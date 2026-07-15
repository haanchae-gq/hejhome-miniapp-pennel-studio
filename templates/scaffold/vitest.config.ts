import path from 'path';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

/**
 * Test-only build. `ray build` never reads this file — it has its own pipeline.
 *
 * The Ray runtime (`@ray-js/ray`) and the device SDK (`@ray-js/panel-sdk`) only
 * exist inside the miniapp host, so both are aliased to stubs under `test/stubs`.
 * Everything below that seam — components, pages, i18n, DP config — is the real
 * source.
 */
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      { find: /^@ray-js\/ray$/, replacement: path.resolve(__dirname, 'test/stubs/ray.tsx') },
      // `src/devices/index.ts` instantiates a real `SmartDeviceModel` and pulls a
      // deep SDK path (`lib/sdm/interceptors/dp-kit`) that only resolves inside
      // the host. Swap the whole module — `useDp` needs nothing from it but
      // `devices.r6.publishDps`.
      {
        find: /^@\/devices$/,
        replacement: path.resolve(__dirname, 'test/stubs/devices.ts'),
      },
      {
        find: /^@ray-js\/panel-sdk$/,
        replacement: path.resolve(__dirname, 'test/stubs/panel-sdk.ts'),
      },
      { find: /^@\//, replacement: `${path.resolve(__dirname, 'src')}/` },
    ],
  },
  // `.less` is compiled for real (the `less` package arrives via @ray-js/cli),
  // so CSS-module class names come out hashed — `styles.on` yields `_on_a473af`.
  // Tests match the local part, not the whole token; see `hasClass` in the suite.
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['test/setup.ts'],
    include: ['test/**/*.test.tsx', 'test/**/*.test.ts'],
  },
});
