# 管理员视频审核

## 本批功能

- 普通用户默认 role=user；管理员权限从数据库实时检查，不由前端或注册请求决定。
- `/admin/videos`：待审核、已通过、已拒绝列表，游标分页、详情、受保护的视频预览、最近 20 条审核历史。
- 人工审核通过/拒绝，拒绝须填写 1–1000 字原因。审核通过不会自动发布，作者自行发布。
- 稿件标题/简介实际变化以及上传文件绑定时递增 content_version。提交审核必须携带查看时的版本；过期版本、非待审核状态返回 409。
- 稿件行锁与作者编辑协调；结果更新和审核记录在同一事务中提交；同一稿件版本有唯一审核记录约束。
- 作者创作中心及投稿详情展示拒绝原因；修改并保存后重新待审核。修改视频文件仍需新建稿件。
- 审核记录包含 reviewer_type=human、审核员 ID、内容版本、决定、原因和时间。没有接入 Agent，也没有授予 Agent 管理员权限。

## 更新并启动

同步前后端代码，包括 go.mod/go.sum。停止旧后端后，在服务器运行：

```bash
cd /home/yujun/projects/live-stream
VIDEO_DEMO_MODE=0 \
VIDEO_UPLOAD_AUTO_APPROVE=0 \
VIDEO_STORAGE_DIR=/home/yujun/projects/live-stream/storage/videos \
go run .
```

继续使用现有 AutoMigrate，新增 users.role、videos.content_version/audit_reason/submitted_at 以及 video_reviews 表。保留数据库和媒体目录，无需删表。已有待审核且文件就绪的真实上传可以直接审核；历史 submitted_at 为空时页面显示创建时间。历史自动通过的记录不补造人工审核日志。演示用的 `/media/output.mp4` 没有后端存储文件，不能使用管理员专用预览，应重新真实上传。

前端执行 `pnpm lint`、`pnpm build`，重启原服务，保留现有监听端口。systemd/Docker 部署需要在实际后端进程配置中关闭两个演示开关。

## 指定管理员（由项目所有者执行）

先正常注册一个单独的审核账号。在正确的项目 MySQL 数据库中执行查询，确认邮箱与 ID：

```sql
SELECT user_id, user_name, user_email, role
FROM users
WHERE user_email = '实际审核账号邮箱';
```

确认目标后，将下面的 `123` 替换为查询出的真实用户 ID，再执行：

```sql
UPDATE users SET role = 'admin' WHERE user_id = 123;
SELECT user_id, user_name, role FROM users WHERE user_id = 123;
```

不要批量提权。程序不会自动指定管理员，也没有公开角色修改接口。撤销身份使用同样的精确 ID 更新为 user；后端每次访问管理员接口都会重新检查身份。

管理员登录后刷新页面，从创作中心左侧“视频审核”进入，或打开：

`http://119.91.119.208:8081/admin/videos`

前端角色只用于展示入口。隐藏按钮不能替代后端校验，普通用户直接调用管理员接口仍返回 403，未登录返回 401。

## API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | /api/v1/admin/videos?status=pending&limit=20&cursor=... | 队列，status 支持 pending/approved/rejected |
| GET | /api/v1/admin/videos/:video_id | 详情、作者名称、最近 20 条审核记录 |
| GET/HEAD | /api/v1/admin/videos/:video_id/media?version=版本号 | 带登录 Cookie 的文件预览，支持 Range |
| POST | /api/v1/admin/videos/:video_id/review | 提交审核 |

审核 JSON 示例：

```json
{"content_version":"4","decision":"rejected","reason":"标题与视频内容不符，请修改标题"}
```

审核人 ID、类型和时间由服务端写入，客户端不能指定。成功返回 200；不存在/已删除 404；版本变化/已处理 409；参数不合法 400。提交使用现有浏览器 Origin 校验。预览使用 private,no-store，不开放 storage/videos 静态目录；已启动的传输和已缓存到客户端的数据不能撤回。

## 两个账号验收

1. 作者上传真实 MP4，状态为 pending，公开详情和媒体接口返回 404。
2. 普通用户请求管理员列表、详情、预览和审核提交，都返回 403；匿名请求均 401。
3. 管理员选中稿件，播放器正常加载且支持拖动；填写原因后拒绝。
4. 作者刷新创作中心和详情，能看到拒绝原因。
5. 作者修改标题/简介并保存，content_version 加一，audit_reason 清空，重新 pending。
6. 管理员保留旧详情时提交审核，返回 409；页面禁止直接重试，刷新后重新查看。
7. 管理员通过新版本，作者刷新后发布；公开视频可以播放。
8. 两个管理员同时提交同一版本，只允许一次成功，并且只有一条审核记录。
9. 删除稿件后，管理员预览与公开媒体接口新请求均返回 404。
10. 下架后未改内容可再次发布；实际改动后需重新审核。重复保存相同内容不重新审核。

浏览器预览使用 Cookie。建议继续使用现有同源 `/api/` 代理；跨域配置需要允许凭据及网站 Origin，浏览器第三方 Cookie 限制也可能影响预览。

## 已做的本地验证

Go 单元测试与 SQL mock 测试覆盖角色判断、路由登录保护、非法审核参数、内容版本变化、稿件状态检查、事务成功与回滚。SQL mock 不是实际 MySQL 并发测试。前端 lint、TypeScript 和生产构建通过；真实服务器双账号、Range 播放及浏览器界面仍需上述验收。

下一步可推进视频 Feed；Agent 接入应使用独立任务身份和权限，不应复用完整管理员账号。
