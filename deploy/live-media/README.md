# L6 真实直播部署与验收

代码提供单台 SRS 的 RTMP 推流、HLS 播放、场次鉴权与对账。后端、SRS 在同一台 Linux 上运行；前端继续由现有 Nginx 代理。不会启动转码任务，适合目前 2 核 4G 开发机。尚不代表完成公网压测、多媒体节点或低延迟 WebRTC。

## 1. 后端环境

在启动 Go 后端的同一个终端设置（systemd 部署则写入仅服务账号可读的 EnvironmentFile）：

```bash
export LIVE_MEDIA_SECRET="$(openssl rand -hex 32)"
export LIVE_PUSH_BASE=rtmp://119.91.119.208:1935/live
export LIVE_PLAY_BASE=/live-media
export LIVE_SRS_API=http://127.0.0.1:1985
```

密钥需要持久保存，重启不要重新生成，否则旧 OBS 凭证失效。不要提交到 Git。后端启动自动迁移 live_sessions、rooms.live_session_id。未配置时开播接口返回 503，其他视频与社区功能可用。升级前结束旧的模拟直播：旧数据没有真实推流，不能作为媒体验收依据。

## 2. 启动 SRS

```bash
cd ~/projects/live-stream
docker compose -f deploy/live-media/compose.yaml up -d
docker compose -f deploy/live-media/compose.yaml logs --tail=50 srs
```

腾讯云安全组放行 TCP 1935（可只允许主播 IP）。不要开放 1985、8080、9001；配置绑定回环地址。已有服务占用这些端口时，必须同步修改 SRS 配置、后端 API 环境及 Nginx 上游。SRS 镜像使用 6 系列，正式上线前应固定经过验收的镜像 digest。

## 3. Nginx 与前端

把同目录 nginx.conf.example 的两个 location 加到现有 8081 公网 server 中，保留其他配置。第二个规则只匹配 `/live/` 下的 m3u8/ts；不要使用宽泛的 `location /live/`，否则会遮蔽前端直播首页：

```bash
sudo nginx -t && sudo systemctl reload nginx
cd frontend
pnpm install --frozen-lockfile
pnpm build
```

按当前项目部署方式重启前端。播放 URL 是公网网页的同源 `/live-media/<本场stream>.m3u8`，不用把 MP4 复制到前端。HTTPS 网站使用同源 HTTPS 媒体代理，避免混合内容。直接访问前端开发端口时也必须配置媒体反向代理，或将 LIVE_PLAY_BASE 设为完整、可跨域访问的媒体 URL。

## 4. OBS 操作

1. 登录作者账号，打开 `/live#studio`，创建房间、点击开始直播。
2. 将页面给出的服务器与推流密钥分别填入 OBS 的自定义服务。密钥框可选中复制；不要把密钥放进截图或日志。
3. 选择 H.264 + AAC、关键帧间隔 2 秒。测试建议 720p / 1500–2500 kbps，在主播电脑编码。
4. OBS 开始推流，回调成功后房间才变为直播中，进入热门 Feed、关注直播并产生一次开播动态。
5. 观众访问 `/rooms/<房间ID>` 或 `/feed`，验证 HLS 与弹幕。HLS 存在数秒延迟，不是 WebRTC。
6. 结束推流后本场永久结束。再次直播点击开始直播获取新密钥，不能复用旧密钥自动重连旧场次。

## 5. 验收清单

- 没有推流时房间不进入 Feed；凭证 15 分钟内首次使用，重复申请未过期凭证不创建新场次。
- 第二个账号不能为作者申请凭证或结束其直播；未经鉴权的 stream / 错误 token 推流被拒。
- 视频可播放、弹幕点赞可用。切换 Feed 卡片后只有活跃卡片保留 HLS 与 WebSocket。
- 作者结束直播会先持久化结束状态，再断开 SRS 推流；SRS 临时不可用时对账任务恢复后补断开。
- OBS 停止时观众状态自动更新，Feed 过滤结束房间。通知丢失由页面每 5 秒读取状态兜底。
- 再次开播得到不同场次与 stream；旧回调不能结束新场次，结束的场次不能被迟到 publish 回调复活。
- 关注账号在 `/live` 看到关注的直播，在 `/dynamic` 看到开播动态。结束后动态仍保留并显示结束，重播新增动态。
- 停止 SRS，恢复后缺失的直播被对账关闭；管理 API 不可用时不根据不完整信息批量关播。首个推流回调后保留 60 秒注册宽限，随后每 20 秒核对。
- 保持 Kafka / Outbox 消费工作，验证同场仅一个 `live.started:<session_id>` 事件。媒体启动与事件写入在同一事务。

自动化真实媒体冒烟：登录后导出作者 Netscape cookie 文件，再执行（脚本会开始并结束指定房间，仅用于测试房间）：

```bash
python3 scripts/test-l6-live-media.py --base-url http://127.0.0.1:9000 \
  --public-base http://119.91.119.208:8081 --cookie /path/author.cookies \
  --room 你的测试房间ID --media /home/yujun/videos/output.mp4
```

需要本机 ffmpeg，输入视频应为 H.264 + AAC；脚本使用流复制减少 CPU 占用。单元测试覆盖权限、重复/迟到回调、事务回滚和鉴权，真实音画、Nginx 与公网访问仍需上述服务器验收。

协议依据：[SRS 6 回调](https://ossrs.io/lts/en-us/docs/v6/doc/http-callback)、[SRS 管理 API](https://ossrs.io/lts/en-us/docs/v6/doc/http-api)、[HLS.js](https://github.com/video-dev/hls.js)。
