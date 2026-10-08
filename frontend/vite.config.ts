import { sites } from '@openai/sites-vite-plugin';
import tailwindcss from '@tailwindcss/postcss';
import vinext from 'vinext';
import { defineConfig, loadEnv, type ProxyOptions } from 'vite';
import hostingConfig from './.openai/hosting.json';

const SITE_CREATOR_PLACEHOLDER_DATABASE_ID =
  '00000000-0000-4000-8000-000000000000';

const { d1, r2 } = hostingConfig;

// macOS Seatbelt 会阻止 FSEvents，因此 Codex 预览需要通过轮询实现热更新。
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
  // 将 Wrangler 和 Miniflare 状态保留在项目目录内。这些是非敏感的工具设置；
  // 应用环境变量应存放在被忽略的 `.env*` 文件中。
  process.env.WRANGLER_WRITE_LOGS ??= 'false';
  process.env.WRANGLER_LOG_PATH ??= '.wrangler/logs';
  process.env.MINIFLARE_REGISTRY_PATH ??= '.wrangler/registry';

  // 导入 Cloudflare 插件时，Wrangler 会记录当时的日志路径。
  const { cloudflare } = await import('@cloudflare/vite-plugin');

  // 除非显式调用 loadEnv，否则 Vite 会在配置求值后才加载 .env 文件。
  // 在此读取这些文件，避免 `.env.local` 中配置的远程后端被静默回退为
  // 前端机器上的 127.0.0.1:9000。
  const env = loadEnv(mode, process.cwd(), 'LIVE_STREAM_');
  const backendTarget = process.env.LIVE_STREAM_BACKEND_URL || env.LIVE_STREAM_BACKEND_URL || 'http://127.0.0.1:9000';
  const websocketOrigin = process.env.LIVE_STREAM_WEBSOCKET_ORIGIN || env.LIVE_STREAM_WEBSOCKET_ORIGIN || 'http://localhost:3000';
  const apiProxy: ProxyOptions = {
    target: backendTarget,
    changeOrigin: true,
    ws: true,
    configure(proxy) {
      // 浏览器与这个可信的同源开发代理通信。
      // 将转发的 WebSocket Origin 规范化为 Go 服务端明确允许的值，
      // 不受 localhost、127 或局域网 URL 的影响。
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
