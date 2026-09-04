import { sites } from '@openai/sites-vite-plugin';
import tailwindcss from '@tailwindcss/postcss';
import vinext from 'vinext';
import { defineConfig, loadEnv, type ProxyOptions } from 'vite';
import hostingConfig from './.openai/hosting.json';

const SITE_CREATOR_PLACEHOLDER_DATABASE_ID =
  '00000000-0000-4000-8000-000000000000';

const { d1, r2 } = hostingConfig;

// macOS Seatbelt blocks FSEvents, so Codex previews need polling for HMR.
const isCodexSeatbeltSandbox = process.env.CODEX_SANDBOX === 'seatbelt';

const localBindingConfig = {
  main: 'vinext/server/fetch-handler',
  compatibility_flags: ['nodejs_compat'],
  d1_databases: d1
    ? [
        {
          binding: d1,
          database_name: 'site-creator-d1',
          database_id: SITE_CREATOR_PLACEHOLDER_DATABASE_ID,
        },
      ]
    : [],
  r2_buckets: r2
    ? [
        {
          binding: r2,
          bucket_name: 'site-creator-r2',
        },
      ]
    : [],
};

export default defineConfig(async ({ mode }) => {
  // Keep Wrangler and Miniflare state project-local. These are non-secret tool
  // settings; application environment belongs in ignored `.env*` files.
  process.env.WRANGLER_WRITE_LOGS ??= 'false';
  process.env.WRANGLER_LOG_PATH ??= '.wrangler/logs';
  process.env.MINIFLARE_REGISTRY_PATH ??= '.wrangler/registry';

  // Wrangler snapshots its log path while the Cloudflare plugin is imported.
  const { cloudflare } = await import('@cloudflare/vite-plugin');

  // Vite loads .env files after config evaluation unless loadEnv is called
  // explicitly. Read them here so a remote backend configured in .env.local
  // does not silently fall back to the frontend machine's 127.0.0.1:9000.
  const env = loadEnv(mode, process.cwd(), 'LIVE_STREAM_');
  const backendTarget = process.env.LIVE_STREAM_BACKEND_URL || env.LIVE_STREAM_BACKEND_URL || 'http://127.0.0.1:9000';
  const websocketOrigin = process.env.LIVE_STREAM_WEBSOCKET_ORIGIN || env.LIVE_STREAM_WEBSOCKET_ORIGIN || 'http://localhost:3000';
  const apiProxy: ProxyOptions = {
    target: backendTarget,
    changeOrigin: true,
    ws: true,
    configure(proxy) {
      // The browser talks to this trusted same-origin development proxy.
      // Normalize the forwarded WebSocket Origin to a value explicitly
      // allowed by the Go server, independent of localhost/127/LAN URL.
      proxy.on('proxyReqWs', (proxyRequest) => {
        proxyRequest.setHeader('Origin', websocketOrigin);
      });
    },
  };

  return {
    css: { postcss: { plugins: [tailwindcss()] } },
    server: {
      ...(isCodexSeatbeltSandbox ? { watch: { useFsEvents: false, usePolling: true } } : {}),
      proxy: {
        '/api': apiProxy,
      },
    },
    plugins: [
      vinext(),
      sites(),
      cloudflare({
        viteEnvironment: { name: 'rsc', childEnvironments: ['ssr'] },
        config: localBindingConfig,
      }),
    ],
  };
});
