# 稿件编辑、下架与软删除

后续更新：管理员审核已实现，详见 [管理员审核开发说明](video-review-development.md)。

本批在现有创作中心接入三个操作，不包含管理员审核后台。

## 实现位置

- `internal/model/video.go`：增加 DeletedAt（GORM 软删除）与只含标题/简介的更新请求。
- `internal/repo/video.go`：作者稿件事务与行锁；软删除按 video_id、owner_id 限制。
- `internal/service/video_manage.go`：状态检查、字段校验、修改后重新审核。
- `internal/handler/video_manage.go`：严格 JSON 字段白名单、登录/来源校验。
- `frontend/components/video-manage-actions.tsx`：下架/删除按钮、二次确认、失败提示。
- `frontend/components/video-submission.tsx`：编辑和保存，未保存时阻止上传或发布。
- `frontend/app/studio/page.tsx`：列表操作后重新查询。

## 服务器更新

1. 同步本批前后端代码，停止旧后端。
2. 保持已有数据库，启动后端时现有 AutoMigrate 会给 videos 表增加 deleted_at 和索引，不需要重建表。
3. 继续验证开发环境直接发布时：

```bash
cd /home/yujun/projects/live-stream
VIDEO_DEMO_MODE=0 \
VIDEO_UPLOAD_AUTO_APPROVE=1 \
VIDEO_STORAGE_DIR=/home/yujun/projects/live-stream/storage/videos \
go run .
```

正式审核模式不要启用 VIDEO_UPLOAD_AUTO_APPROVE。该模式下，标题或简介发生实际变更后，audit_status 回到 pending；当前尚无审核后台，不会自行变为通过。开发开关只在媒体已经 ready 且有播放地址时允许编辑后通过。没有实际变更的保存不改变审核状态。

4. 前端执行 `pnpm lint`、`pnpm build`，重启原前端服务，沿用已有端口和 Nginx 配置。需要同步新前端和新后端，否则可能遇到 404/405。

## 页面验收

1. 使用作者账号打开 `/studio`，选择一条测试草稿进入管理页，修改标题和简介，点击“保存修改”。刷新后内容应保持。
2. 已发布稿件的文本输入框应禁用；点“下架”，先取消验证无变化，再确认验证退回草稿。
3. 下架后公开列表不显示，公开详情和媒体接口的新请求返回 404；作者管理页仍可访问。
4. 编辑下架后的稿件，开发模式验证重新发布；正式模式验证实际内容变更后回到待审核。
5. 对测试稿件点“删除”，先取消，再确认。列表移除、作者详情与公开详情返回 404，后续上传/发布均不能恢复它。
6. 换账号访问该稿件的编辑/下架/删除接口应返回 404；匿名访问返回 401。通过修改前端按钮不能绕过限制。
7. 删除时保留数据库行与磁盘文件，deleted_at 非空；不提供自助恢复，也不实施物理文件清理。

## Linux 接口验证

先用实际作者账号登录获得 Cookie 文件，把文件路径设为 COOKIE_FILE；把可操作的测试稿件 ID 设为 VIDEO_ID。不要复制未替换的示例 ID，更不要针对要保留的真实稿件测试删除。

```bash
# 编辑草稿（已发布时返回 409）
curl -sS -b "$COOKIE_FILE" -X PATCH \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID" \
  -H 'Content-Type: application/json' \
  -d '{"title":"编辑后的测试标题","description":"新的简介"}'

# 下架，重复下架草稿也返回成功
curl -sS -b "$COOKIE_FILE" -X POST \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID/unpublish"

# 检查下架后的新播放请求：预期 404
curl -sS -o /dev/null -w '%{http_code}\n' \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID/media"

# 仅对准备删除的测试稿件执行
curl -sS -b "$COOKIE_FILE" -X DELETE \
  "http://127.0.0.1:9000/api/v1/videos/$VIDEO_ID"
```

## 边界和验证范围

- 软删除通过 GORM 默认作用域影响作者列表、公开列表、详情、上传绑定与发布条件；媒体接口先校验公开状态再打开文件。
- 已经开始的媒体响应、客户端缓存/下载内容不会被撤回。如果配置过 Nginx/CDN 独立缓存，也需要同步配置失效规则。
- 老的 `/media/output.mp4` 是前端公开测试素材，删除视频记录不会删除或保护该静态文件；真实上传使用受校验的 `/api/v1/videos/:id/media`。
- 当前编辑采用事务行锁协调状态，并发编辑是后提交覆盖前提交；尚未提供版本冲突合并。
- 本地测试覆盖状态规则、非法字段、匿名路由保护与软删除 SQL 作用域；SQL 测试不连接数据库，真实 MySQL、跨账号、浏览器流程仍需服务器验收。

下一批：管理员角色、待审核列表、视频预览、通过/拒绝、审核理由与操作记录。
