import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Сборка кладётся в web/dist и встраивается в Go-бинарник.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "../dist", emptyOutDir: true, chunkSizeWarningLimit: 900, sourcemap: false },
  server: {
    port: 5173,
    proxy: { "/api": { target: "http://localhost:8080", changeOrigin: false }, "/pay": "http://localhost:8080", "/r/": "http://localhost:8080" },
  },
});
