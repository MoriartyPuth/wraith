import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Dev server proxies the API (incl. WebSocket) to WRAITH Core so the browser
// talks to a single origin and Core needs no CORS. Override the target with
// WRAITH_CORE_URL in the environment or a .env file.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "WRAITH_");
  const core = env.WRAITH_CORE_URL ?? "http://127.0.0.1:8088";
  return {
    plugins: [react(), tailwindcss()],
    server: {
      host: "127.0.0.1",
      port: 5173,
      proxy: { "/api": { target: core, ws: true } },
    },
  };
});
