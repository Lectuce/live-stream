# 创作中心第一阶段

后续更新：编辑、下架与软删除现已接入，部署和验收见 [稿件管理开发说明](video-management-development.md)。下文保留第一阶段的接口与验收说明。

已实现：自己的投稿列表、游标分页、作者详情、恢复草稿、继续上传、刷新审核状态、发布已通过审核的视频。未实现：编辑、删除、下架、审核后台。已有待审核视频不会自动审核通过。

## 代码位置

- `internal/repo/video.go`：按登录用户 owner_id 查询列表和详情，详情同时限制 video_id。
- `internal/service/video.go`：作者详情和分页，返回 has_media 而不暴露文件名。
- `internal/handler/video.go`：登录信息、分页参数、private/no-store 响应头。
- `internal/rousters/rousters.go`：GET /api/v1/videos/mine 和 GET /api/v1/videos/mine/:video_id，使用 Auth。
- `frontend/app/studio/page.tsx`：创作中心、刷新状态、翻页、继续投稿入口。
- `frontend/app/upload/page.tsx`、`frontend/components/video-submission.tsx`：读取 video_id，恢复自己的草稿；创建成功后把 ID 留在地址栏。
- `frontend/lib/video-status.ts`：展示状态和操作按钮判断。最终权限仍由后端判断。

本阶段没有新增数据库字段。OwnerVideo/OwnerVideoPage 是响应结构，不是新表。

## 部署和操作

1. 同步本批代码到 Ubuntu，停止旧后端，再在项目根目录启动。需要验证新投稿直接发布时使用：

   ```bash
   cd /home/yujun/projects/live-stream
   VIDEO_DEMO_MODE=0 \
   VIDEO_UPLOAD_AUTO_APPROVE=1 \
   VIDEO_STORAGE_DIR=/home/yujun/projects/live-stream/storage/videos \
   go run .
   ```

   这是开发环境开关；只影响后续上传。没有开关时上传进入 pending，已有 pending 视频保持不变。systemd/Docker 部署应修改实际服务的环境配置。

2. 前端执行 `pnpm lint`、`pnpm build`。开发服务更新代码后访问；生产服务重建后重启原服务，保留原有监听端口和 Nginx 配置。
3. 登录原投稿账号，打开 `http://119.91.119.208:8081/studio`。
4. 未上传草稿点击“继续投稿”，选择文件上传；已有文件不会要求重复上传。
5. 待审核投稿可查看和刷新状态；可发布投稿点击“继续发布”后发布；已发布投稿可直接观看。
6. 新建投稿完成草稿创建后，地址栏包含 video_id。刷新会恢复该草稿，不会创建新记录。若创建响应丢失，先去创作中心找回，不要反复新建。

## 验收

- 使用原账号能看到之前上传的待审核视频，标题、简介、状态正确。
- 上传中断后刷新或从创作中心返回，可对同一草稿重试；已上传成功但响应丢失时，刷新状态能找回文件。
- 关闭页面重新打开 `/upload?video_id=实际ID`，恢复原稿；地址错误或非本人投稿显示错误，不创建草稿。
- 多于 20 条投稿可翻页，页内/跨页不重复，最后一页 has_more=false、next_cursor 为空。
- 账号 B 不能在列表中看到 A 的投稿；用 B 登录访问 A 的作者详情返回 404。匿名访问作者列表/详情返回 401。
- 待审核/草稿仍无法通过公开详情和媒体接口访问（404）；公开列表只返回已发布且审核通过的视频。
- 登录账号切换时，原账号的投稿列表和表单状态被清空。
- 开启开发自动通过后，新上传视频可发布；发布后刷新页面不会变回草稿。

可以在 Linux 用已有登录 Cookie 文件检查接口（COOKIE_FILE 必须指向真实文件）：

```bash
curl -sS -b "$COOKIE_FILE" \
  'http://127.0.0.1:9000/api/v1/videos/mine?limit=20' | python3 -m json.tool

# VIDEO_ID 使用上一条响应中的 video_id；保持字符串，不用 JS Number 转换。
curl -sS -b "$COOKIE_FILE" \
  "http://127.0.0.1:9000/api/v1/videos/mine/$VIDEO_ID" | python3 -m json.tool

curl -sS -o /dev/null -w '%{http_code}\n' \
  'http://127.0.0.1:9000/api/v1/videos/mine'
# 预期 401
```

本地 Go 测试检查路由登录保护、分页参数和响应序列化；不替代真实 MySQL 数据隔离与浏览器上传联调。

下一阶段：作者编辑/下架/软删除，再增加管理员角色、审核通过/拒绝和审核记录。
