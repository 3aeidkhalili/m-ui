import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// در حالت توسعه، درخواست‌های /api به بک‌اند Go پروکسی می‌شوند.
export default defineConfig({
  plugins: [react()],
  base: "./",
  resolve: {
    // روی درایوِ نگاشته‌شده (subst) به مسیر WSL، از تبدیلِ realpath به مسیر UNC جلوگیری کن
    preserveSymlinks: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8000",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
  },
});
