# LIVE NOW 前端

对应 `live-stream` Go 后端的 React/Vinext 前端，包含：

- 注册、登录、退出和 Session 恢复；
- 直播房间列表、搜索、创建、改名、开播和关播；
- WebSocket 实时弹幕、点赞、在线人数；
- `room_seq` 缺口检测、断线重连、Replay 自动补拉；
- 热门房间 `sampled` 模式下的推拉结合；
- 历史弹幕分页和直播间弹幕覆盖层；
- 桌面端与移动端响应式布局。

## 本机启动

后端运行在本机 `9000` 端口时：

```powershell
Set-Location D:\GoProjects\live-stream\frontend
$env:Path = "C:\Users\Ambition\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin;C:\Users\Ambition\.cache\codex-runtimes\codex-primary-runtime\dependencies\bin\fallback;$env:Path"
pnpm dev
```

浏览器打开终端显示的地址，通常为 `http://localhost:3000`。

## Linux 服务器启动

前后端运行在同一台 Linux 服务器时：

```bash
cd ~/projects/live-stream/frontend
pnpm install --frozen-lockfile

LIVE_STREAM_BACKEND_URL=http://127.0.0.1:9000 \
pnpm dev --hostname 0.0.0.0 --port 3000
```

Vinext 使用 `--hostname`（不是 Vite CLI 的 `--host`）。局域网浏览器访问 `http://服务器地址:3000`，后端白名单需要包含这个 Origin。

## 连接局域网服务器

例如后端位于 `172.21.15.57:9000`：

```powershell
Set-Location D:\GoProjects\live-stream\frontend
$env:Path = "C:\Users\Ambition\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin;C:\Users\Ambition\.cache\codex-runtimes\codex-primary-runtime\dependencies\bin\fallback;$env:Path"
$env:LIVE_STREAM_BACKEND_URL = "http://172.21.15.57:9000"
pnpm dev
```

前端开发服务器会将 `/api` 和 WebSocket 请求反向代理到后端。不要在连接设置里改成局域网地址，保留页面当前同源地址即可；这样登录 Cookie 才不会被浏览器的跨站策略拦截。

后端 `config/config.yaml` 的 `server.allowed_origins` 需要包含浏览器实际打开的前端 Origin，例如 `http://localhost:3000`。
