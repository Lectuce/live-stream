# 视频投稿开发验证

本阶段实现真实 MP4 上传、探测、开发环境发布和播放。暂不包含转码 Worker、封面生成、创作中心、审核后台或断点续传；列表按创建 ID 倒序分页，尚非推荐 Feed。

## 启动

在 Ubuntu 安装 FFprobe（包含在 ffmpeg 包中），在项目根目录启动后端。先停止旧后端进程，避免端口占用；由服务管理器运行时应修改对应服务的环境配置。

```bash
sudo apt update
sudo apt install -y ffmpeg
ffprobe -version

cd /home/yujun/projects/live-stream
VIDEO_DEMO_MODE=0 \
VIDEO_UPLOAD_AUTO_APPROVE=1 \
VIDEO_STORAGE_DIR=/home/yujun/projects/live-stream/storage/videos \
go run .
```

`VIDEO_UPLOAD_AUTO_APPROVE=1` 仅在受控开发环境使用：探测通过后允许作者发布。不设置时上传后的审核状态为 pending，现阶段没有审核后台，不能直接发布。客户端无法提交 ready/approved 或任意存储路径。

上传文件不再复制到前端 public；旧 `/media/output.mp4` 仍是公开测试素材。新文件由后端写入上述目录（已在 Git 忽略），数据库只保存内部生成的文件名和播放地址。目录必须持久化且由运行后端的用户可读写，数据库和文件需要一起保留。此阶段没有用户存储总配额、失败任务恢复、孤儿文件回收与 FFprobe 沙箱，不能直接作为开放投稿的生产部署。

## Nginx

用 `sudo nginx -T` 找到当前监听 8081 的 server，在该块配置：

```nginx
client_max_body_size 101m;
client_body_timeout 180s;
```

在现有转发 `/api/` 的 location 中增加以下配置，保留原 proxy_pass 等设置：

```nginx
proxy_request_buffering off;
proxy_read_timeout 240s;
proxy_send_timeout 240s;
```

验证后重载：

```bash
sudo nginx -t && sudo systemctl reload nginx
```

浏览器 Origin 必须在后端 `server.allowed_origins` 内（当前网站为 `http://119.91.119.208:8081`）。上传接口额外验证 Origin，防止跨站 multipart 请求借用登录 Cookie。

## 网页操作

更新前端代码；开发模式热更新，生产模式重新构建并重启原服务。通过顶部“投稿”进入 `/upload`，先登录，然后填写标题/简介、选择电脑上的 MP4、上传、等待探测通过、点击发布、进入详情播放。

格式限制：非空、最多 100 MiB、MP4 容器、一个 H.264/yuv420p 视频轨、至多一个 AAC 音轨（无音频也可）、最长 1 小时、宽高均不大于 1920 且总像素不超过 1920×1080。暂不支持字幕/额外数据轨。不同视频记录存储不同文件；当前单个后端实例同时只接受一个上传，忙时返回 429。

一次请求创建一个草稿；上传失败可在本页换文件重试；发布失败可重试发布。成功上传后的文件不能被覆盖。现可通过 `/studio` 找回草稿，或用 `/upload?video_id=实际ID` 恢复。上传响应丢失时先刷新投稿状态，确认是否已保存，避免无限重传。元数据编辑和删除仍待实现。详见 [创作中心开发说明](video-studio-development.md)。

## Linux 接口联调

已有账号登录所得 Cookie 文件通过 `-b` 传入。上传体是 multipart，不能手动设置 JSON Content-Type。

```text
POST /api/v1/videos                         创建草稿（title, description）
POST /api/v1/videos/:video_id/upload         作者上传：multipart 字段 file
POST /api/v1/videos/:video_id/publish        作者发布
GET  /api/v1/videos                         公共视频列表
GET  /api/v1/videos/:video_id                公共视频详情
GET/HEAD /api/v1/videos/:video_id/media      已公开文件，支持 Range
```

例如把创建草稿响应的实际 ID 保存为 `VIDEO_ID`，Cookie 路径保存为 `COOKIE_FILE` 后：

```bash
curl -sS -b "$COOKIE_FILE" \
  -F 'file=@/home/yujun/videos/output.mp4' \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID/upload"

curl -sS -b "$COOKIE_FILE" -X POST \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID/publish"

curl -sS -D - -o /dev/null \
  -H 'Range: bytes=0-1023' \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID/media"
```

## 验收

- 作者上传两个不同 MP4，发布后列表和详情分别播放对应文件，可拖动进度；Range 返回 206。
- 草稿、未审核视频的公开详情及媒体接口返回 404；其他用户上传/发布返回 403；未登录上传返回 401。
- 大于 100 MiB 返回 413；伪装扩展名、编码不兼容返回 400/415；文件和 DB 不应出现半完成的可发布记录。
- 同时两个上传只接受一个，另一个 429；重试发布不改变已发布视频的发布时间。
- ffprobe 缺失或超时返回失败，草稿仍可重试；服务器日志可定位错误。

本地代码检查不代表这些真实 MySQL、媒体文件、浏览器和 Nginx 场景全部已验收。记录服务器结果后再开发创作中心与评论。
