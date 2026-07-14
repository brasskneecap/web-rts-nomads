// vite.config.ts
import { defineConfig } from "file:///C:/Personal%20Dev/webrts/client/src/game-portal/node_modules/vite/dist/node/index.js";
import vue from "file:///C:/Personal%20Dev/webrts/client/src/game-portal/node_modules/@vitejs/plugin-vue/dist/index.mjs";
import path from "path";
import { execSync } from "node:child_process";
var __vite_injected_original_dirname = "C:\\Personal Dev\\webrts\\client\\src\\game-portal";
var GO_SERVER = "http://localhost:8080";
function resolveAppVersion(mode) {
  if (mode === "development") return "dev";
  if (process.env.NOMADS_VERSION && process.env.NOMADS_VERSION.length > 0) {
    return process.env.NOMADS_VERSION;
  }
  try {
    return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] }).toString().trim() || "unknown";
  } catch {
    return "unknown";
  }
}
var vite_config_default = defineConfig(({ mode }) => ({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": path.resolve(__vite_injected_original_dirname, "./src")
    }
  },
  define: {
    __APP_VERSION__: JSON.stringify(resolveAppVersion(mode))
  },
  // Default chunkSizeWarningLimit (500 kB) is tuned for web delivery. Our
  // packaged build loads the SPA from 127.0.0.1 over loopback inside the
  // Tauri webview, so per-chunk bytes don't matter the same way. Bumped to
  // 1500 kB so the warning fires only on genuinely runaway bundles.
  build: {
    chunkSizeWarningLimit: 1500
  },
  server: {
    host: true,
    allowedHosts: true,
    // Every server route the SPA calls must be listed here, or in `npm run dev`
    // it silently hits the Vite dev server instead of Go and 404s. This is not
    // theoretical: /units shipped without an entry, so the unit editor's Save
    // and Delete were dead in dev (GET /catalog/units worked, because /catalog
    // IS proxied — which is exactly why it went unnoticed). Adding a write
    // endpoint on the server means adding its prefix here.
    proxy: {
      "/ws": { target: GO_SERVER, ws: true, changeOrigin: true },
      "/health": { target: GO_SERVER, changeOrigin: true },
      "/api": { target: GO_SERVER, changeOrigin: true },
      "/catalog": { target: GO_SERVER, changeOrigin: true },
      "/assets": { target: GO_SERVER, changeOrigin: true },
      "/maps": { target: GO_SERVER, changeOrigin: true },
      "/items": { target: GO_SERVER, changeOrigin: true },
      "/units": { target: GO_SERVER, changeOrigin: true },
      "/unit-art": { target: GO_SERVER, changeOrigin: true },
      "/factions": { target: GO_SERVER, changeOrigin: true },
      "/abilities": { target: GO_SERVER, changeOrigin: true },
      "/paths": { target: GO_SERVER, changeOrigin: true },
      "/perks": { target: GO_SERVER, changeOrigin: true },
      "/matches": { target: GO_SERVER, changeOrigin: true },
      "/lobbies": { target: GO_SERVER, changeOrigin: true }
    }
  },
  test: {
    environment: "happy-dom",
    include: ["src/**/*.test.ts"]
  }
}));
export {
  vite_config_default as default
};
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsidml0ZS5jb25maWcudHMiXSwKICAic291cmNlc0NvbnRlbnQiOiBbImNvbnN0IF9fdml0ZV9pbmplY3RlZF9vcmlnaW5hbF9kaXJuYW1lID0gXCJDOlxcXFxQZXJzb25hbCBEZXZcXFxcd2VicnRzXFxcXGNsaWVudFxcXFxzcmNcXFxcZ2FtZS1wb3J0YWxcIjtjb25zdCBfX3ZpdGVfaW5qZWN0ZWRfb3JpZ2luYWxfZmlsZW5hbWUgPSBcIkM6XFxcXFBlcnNvbmFsIERldlxcXFx3ZWJydHNcXFxcY2xpZW50XFxcXHNyY1xcXFxnYW1lLXBvcnRhbFxcXFx2aXRlLmNvbmZpZy50c1wiO2NvbnN0IF9fdml0ZV9pbmplY3RlZF9vcmlnaW5hbF9pbXBvcnRfbWV0YV91cmwgPSBcImZpbGU6Ly8vQzovUGVyc29uYWwlMjBEZXYvd2VicnRzL2NsaWVudC9zcmMvZ2FtZS1wb3J0YWwvdml0ZS5jb25maWcudHNcIjsvLy8gPHJlZmVyZW5jZSB0eXBlcz1cInZpdGVzdFwiIC8+XHJcbmltcG9ydCB7IGRlZmluZUNvbmZpZyB9IGZyb20gJ3ZpdGUnXHJcbmltcG9ydCB2dWUgZnJvbSAnQHZpdGVqcy9wbHVnaW4tdnVlJ1xyXG5pbXBvcnQgcGF0aCBmcm9tICdwYXRoJ1xyXG5pbXBvcnQgeyBleGVjU3luYyB9IGZyb20gJ25vZGU6Y2hpbGRfcHJvY2VzcydcclxuXHJcbmNvbnN0IEdPX1NFUlZFUiA9ICdodHRwOi8vbG9jYWxob3N0OjgwODAnXHJcblxyXG4vLyBcdTAwQTcxNSB0YXNrIDE1Ljg6IF9fQVBQX1ZFUlNJT05fXyBpbmplY3Rpb24uIFJlc29sdmVkIGF0IGJ1aWxkIHRpbWUgaW4gdGhpc1xyXG4vLyBwcmlvcml0eSBvcmRlcjpcclxuLy8gICAxLiBOT01BRFNfVkVSU0lPTiBlbnYgdmFyIChDSSAvIHJlbGVhc2UgdGFncylcclxuLy8gICAyLiBzaG9ydCBnaXQgU0hBIGZyb20gZ2l0IHJldi1wYXJzZSAtLXNob3J0IEhFQUQgaWYgaW4gYSBnaXQgY2hlY2tvdXRcclxuLy8gICAzLiBsaXRlcmFsIFwidW5rbm93blwiICh0YXJiYWxsIC8gbm8tZ2l0IGJ1aWxkcylcclxuLy8gYG5wbSBydW4gZGV2YCBhbmQgYHRhdXJpOmRldmAgb3ZlcnJpZGUgdG8gXCJkZXZcIiB2aWEgdGhlIGRldi1tb2RlIGJyYW5jaCBiZWxvdy5cclxuZnVuY3Rpb24gcmVzb2x2ZUFwcFZlcnNpb24obW9kZTogc3RyaW5nKTogc3RyaW5nIHtcclxuICBpZiAobW9kZSA9PT0gJ2RldmVsb3BtZW50JykgcmV0dXJuICdkZXYnXHJcbiAgaWYgKHByb2Nlc3MuZW52Lk5PTUFEU19WRVJTSU9OICYmIHByb2Nlc3MuZW52Lk5PTUFEU19WRVJTSU9OLmxlbmd0aCA+IDApIHtcclxuICAgIHJldHVybiBwcm9jZXNzLmVudi5OT01BRFNfVkVSU0lPTlxyXG4gIH1cclxuICB0cnkge1xyXG4gICAgcmV0dXJuIGV4ZWNTeW5jKCdnaXQgcmV2LXBhcnNlIC0tc2hvcnQgSEVBRCcsIHsgc3RkaW86IFsnaWdub3JlJywgJ3BpcGUnLCAnaWdub3JlJ10gfSlcclxuICAgICAgLnRvU3RyaW5nKClcclxuICAgICAgLnRyaW0oKSB8fCAndW5rbm93bidcclxuICB9IGNhdGNoIHtcclxuICAgIHJldHVybiAndW5rbm93bidcclxuICB9XHJcbn1cclxuXHJcbmV4cG9ydCBkZWZhdWx0IGRlZmluZUNvbmZpZygoeyBtb2RlIH0pID0+ICh7XHJcbiAgcGx1Z2luczogW3Z1ZSgpXSxcclxuICByZXNvbHZlOiB7XHJcbiAgICBhbGlhczoge1xyXG4gICAgICAnQCc6IHBhdGgucmVzb2x2ZShfX2Rpcm5hbWUsICcuL3NyYycpLFxyXG4gICAgfSxcclxuICB9LFxyXG4gIGRlZmluZToge1xyXG4gICAgX19BUFBfVkVSU0lPTl9fOiBKU09OLnN0cmluZ2lmeShyZXNvbHZlQXBwVmVyc2lvbihtb2RlKSksXHJcbiAgfSxcclxuICAvLyBEZWZhdWx0IGNodW5rU2l6ZVdhcm5pbmdMaW1pdCAoNTAwIGtCKSBpcyB0dW5lZCBmb3Igd2ViIGRlbGl2ZXJ5LiBPdXJcclxuICAvLyBwYWNrYWdlZCBidWlsZCBsb2FkcyB0aGUgU1BBIGZyb20gMTI3LjAuMC4xIG92ZXIgbG9vcGJhY2sgaW5zaWRlIHRoZVxyXG4gIC8vIFRhdXJpIHdlYnZpZXcsIHNvIHBlci1jaHVuayBieXRlcyBkb24ndCBtYXR0ZXIgdGhlIHNhbWUgd2F5LiBCdW1wZWQgdG9cclxuICAvLyAxNTAwIGtCIHNvIHRoZSB3YXJuaW5nIGZpcmVzIG9ubHkgb24gZ2VudWluZWx5IHJ1bmF3YXkgYnVuZGxlcy5cclxuICBidWlsZDoge1xyXG4gICAgY2h1bmtTaXplV2FybmluZ0xpbWl0OiAxNTAwLFxyXG4gIH0sXHJcbiAgc2VydmVyOiB7XHJcbiAgICBob3N0OiB0cnVlLFxyXG4gICAgYWxsb3dlZEhvc3RzOiB0cnVlLFxyXG4gICAgLy8gRXZlcnkgc2VydmVyIHJvdXRlIHRoZSBTUEEgY2FsbHMgbXVzdCBiZSBsaXN0ZWQgaGVyZSwgb3IgaW4gYG5wbSBydW4gZGV2YFxyXG4gICAgLy8gaXQgc2lsZW50bHkgaGl0cyB0aGUgVml0ZSBkZXYgc2VydmVyIGluc3RlYWQgb2YgR28gYW5kIDQwNHMuIFRoaXMgaXMgbm90XHJcbiAgICAvLyB0aGVvcmV0aWNhbDogL3VuaXRzIHNoaXBwZWQgd2l0aG91dCBhbiBlbnRyeSwgc28gdGhlIHVuaXQgZWRpdG9yJ3MgU2F2ZVxyXG4gICAgLy8gYW5kIERlbGV0ZSB3ZXJlIGRlYWQgaW4gZGV2IChHRVQgL2NhdGFsb2cvdW5pdHMgd29ya2VkLCBiZWNhdXNlIC9jYXRhbG9nXHJcbiAgICAvLyBJUyBwcm94aWVkIFx1MjAxNCB3aGljaCBpcyBleGFjdGx5IHdoeSBpdCB3ZW50IHVubm90aWNlZCkuIEFkZGluZyBhIHdyaXRlXHJcbiAgICAvLyBlbmRwb2ludCBvbiB0aGUgc2VydmVyIG1lYW5zIGFkZGluZyBpdHMgcHJlZml4IGhlcmUuXHJcbiAgICBwcm94eToge1xyXG4gICAgICAnL3dzJzogeyB0YXJnZXQ6IEdPX1NFUlZFUiwgd3M6IHRydWUsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL2hlYWx0aCc6IHsgdGFyZ2V0OiBHT19TRVJWRVIsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL2FwaSc6IHsgdGFyZ2V0OiBHT19TRVJWRVIsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL2NhdGFsb2cnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgICAgJy9hc3NldHMnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgICAgJy9tYXBzJzogeyB0YXJnZXQ6IEdPX1NFUlZFUiwgY2hhbmdlT3JpZ2luOiB0cnVlIH0sXHJcbiAgICAgICcvaXRlbXMnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgICAgJy91bml0cyc6IHsgdGFyZ2V0OiBHT19TRVJWRVIsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL3VuaXQtYXJ0JzogeyB0YXJnZXQ6IEdPX1NFUlZFUiwgY2hhbmdlT3JpZ2luOiB0cnVlIH0sXHJcbiAgICAgICcvZmFjdGlvbnMnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgICAgJy9hYmlsaXRpZXMnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgICAgJy9wYXRocyc6IHsgdGFyZ2V0OiBHT19TRVJWRVIsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL3BlcmtzJzogeyB0YXJnZXQ6IEdPX1NFUlZFUiwgY2hhbmdlT3JpZ2luOiB0cnVlIH0sXHJcbiAgICAgICcvbWF0Y2hlcyc6IHsgdGFyZ2V0OiBHT19TRVJWRVIsIGNoYW5nZU9yaWdpbjogdHJ1ZSB9LFxyXG4gICAgICAnL2xvYmJpZXMnOiB7IHRhcmdldDogR09fU0VSVkVSLCBjaGFuZ2VPcmlnaW46IHRydWUgfSxcclxuICAgIH0sXHJcbiAgfSxcclxuICB0ZXN0OiB7XHJcbiAgICBlbnZpcm9ubWVudDogJ2hhcHB5LWRvbScsXHJcbiAgICBpbmNsdWRlOiBbJ3NyYy8qKi8qLnRlc3QudHMnXSxcclxuICB9LFxyXG59KSlcclxuIl0sCiAgIm1hcHBpbmdzIjogIjtBQUNBLFNBQVMsb0JBQW9CO0FBQzdCLE9BQU8sU0FBUztBQUNoQixPQUFPLFVBQVU7QUFDakIsU0FBUyxnQkFBZ0I7QUFKekIsSUFBTSxtQ0FBbUM7QUFNekMsSUFBTSxZQUFZO0FBUWxCLFNBQVMsa0JBQWtCLE1BQXNCO0FBQy9DLE1BQUksU0FBUyxjQUFlLFFBQU87QUFDbkMsTUFBSSxRQUFRLElBQUksa0JBQWtCLFFBQVEsSUFBSSxlQUFlLFNBQVMsR0FBRztBQUN2RSxXQUFPLFFBQVEsSUFBSTtBQUFBLEVBQ3JCO0FBQ0EsTUFBSTtBQUNGLFdBQU8sU0FBUyw4QkFBOEIsRUFBRSxPQUFPLENBQUMsVUFBVSxRQUFRLFFBQVEsRUFBRSxDQUFDLEVBQ2xGLFNBQVMsRUFDVCxLQUFLLEtBQUs7QUFBQSxFQUNmLFFBQVE7QUFDTixXQUFPO0FBQUEsRUFDVDtBQUNGO0FBRUEsSUFBTyxzQkFBUSxhQUFhLENBQUMsRUFBRSxLQUFLLE9BQU87QUFBQSxFQUN6QyxTQUFTLENBQUMsSUFBSSxDQUFDO0FBQUEsRUFDZixTQUFTO0FBQUEsSUFDUCxPQUFPO0FBQUEsTUFDTCxLQUFLLEtBQUssUUFBUSxrQ0FBVyxPQUFPO0FBQUEsSUFDdEM7QUFBQSxFQUNGO0FBQUEsRUFDQSxRQUFRO0FBQUEsSUFDTixpQkFBaUIsS0FBSyxVQUFVLGtCQUFrQixJQUFJLENBQUM7QUFBQSxFQUN6RDtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUEsRUFLQSxPQUFPO0FBQUEsSUFDTCx1QkFBdUI7QUFBQSxFQUN6QjtBQUFBLEVBQ0EsUUFBUTtBQUFBLElBQ04sTUFBTTtBQUFBLElBQ04sY0FBYztBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBLElBT2QsT0FBTztBQUFBLE1BQ0wsT0FBTyxFQUFFLFFBQVEsV0FBVyxJQUFJLE1BQU0sY0FBYyxLQUFLO0FBQUEsTUFDekQsV0FBVyxFQUFFLFFBQVEsV0FBVyxjQUFjLEtBQUs7QUFBQSxNQUNuRCxRQUFRLEVBQUUsUUFBUSxXQUFXLGNBQWMsS0FBSztBQUFBLE1BQ2hELFlBQVksRUFBRSxRQUFRLFdBQVcsY0FBYyxLQUFLO0FBQUEsTUFDcEQsV0FBVyxFQUFFLFFBQVEsV0FBVyxjQUFjLEtBQUs7QUFBQSxNQUNuRCxTQUFTLEVBQUUsUUFBUSxXQUFXLGNBQWMsS0FBSztBQUFBLE1BQ2pELFVBQVUsRUFBRSxRQUFRLFdBQVcsY0FBYyxLQUFLO0FBQUEsTUFDbEQsVUFBVSxFQUFFLFFBQVEsV0FBVyxjQUFjLEtBQUs7QUFBQSxNQUNsRCxhQUFhLEVBQUUsUUFBUSxXQUFXLGNBQWMsS0FBSztBQUFBLE1BQ3JELGFBQWEsRUFBRSxRQUFRLFdBQVcsY0FBYyxLQUFLO0FBQUEsTUFDckQsY0FBYyxFQUFFLFFBQVEsV0FBVyxjQUFjLEtBQUs7QUFBQSxNQUN0RCxVQUFVLEVBQUUsUUFBUSxXQUFXLGNBQWMsS0FBSztBQUFBLE1BQ2xELFVBQVUsRUFBRSxRQUFRLFdBQVcsY0FBYyxLQUFLO0FBQUEsTUFDbEQsWUFBWSxFQUFFLFFBQVEsV0FBVyxjQUFjLEtBQUs7QUFBQSxNQUNwRCxZQUFZLEVBQUUsUUFBUSxXQUFXLGNBQWMsS0FBSztBQUFBLElBQ3REO0FBQUEsRUFDRjtBQUFBLEVBQ0EsTUFBTTtBQUFBLElBQ0osYUFBYTtBQUFBLElBQ2IsU0FBUyxDQUFDLGtCQUFrQjtBQUFBLEVBQzlCO0FBQ0YsRUFBRTsiLAogICJuYW1lcyI6IFtdCn0K
