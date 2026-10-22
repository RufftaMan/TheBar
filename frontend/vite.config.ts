import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

const outDir = resolve(__dirname, '../backend/web/dist');

export default defineConfig({
  plugins: [
    svelte(),
    {
      // emptyOutDir wipes the folder; keep the placeholder git tracks
      name: 'keep-gitkeep',
      closeBundle() {
        writeFileSync(resolve(outDir, '.gitkeep'), '');
      },
    },
  ],
  build: {
    outDir,
    emptyOutDir: true,
    target: 'es2022',
    cssCodeSplit: true,
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:18800', changeOrigin: false },
      '/icons': { target: 'http://127.0.0.1:18800' },
    },
  },
});
