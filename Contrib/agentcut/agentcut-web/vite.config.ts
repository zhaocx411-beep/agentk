/// <reference types="vitest/config" />
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    // 本地联调（VITE_USE_REAL_API=1）时把 /v1/* 转发给 cut-apiserver，
    // 避免浏览器直连 8080 触发 CORS、也不用改后端加 CORS 中间件。
    // SSE（/stream）走同一个代理，Vite 默认按流式转发，不需要额外配置。
    proxy: {
      "/v1": {
        target: process.env.AGENTCUT_API_PROXY_TARGET ?? "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
