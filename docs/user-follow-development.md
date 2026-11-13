# L3-A：关注创作者与公开主页

本批实现关注关系、公开作者资料、公开视频分页、作者主页和视频详情关注按钮。没有实现关注动态、关注/粉丝列表、个人资料编辑、图文或直播间关注入口。这些仍在总计划中，不能把 L3 整体标为完成。

## 1. 数据和代码

- `internal/model/user_follow.go`：`user_follows` 表，`(follower_id, followee_id)` 组合唯一；`followee_id` 单独索引支持粉丝计数。取消关注物理删除关系，重新关注可重新插入。
- `internal/model/user.go`：增加 `bio` 简介。已有账号可以为空，目前无资料编辑入口。
- `internal/dao/db.go`：继续用 AutoMigrate，新建表并添加字段，无需删除数据库。
- `internal/repo/user_follow.go`：插入使用唯一键冲突时不改变关系；删除限定两个 ID。关注数由关系表 COUNT 得出，不另维护容易重复累加的计数字段。状态与粉丝总数在一条 SELECT 中读取。
- `internal/service/user_follow.go`：禁止自己关注自己，校验目标存在；组装公开主页与游标分页。
- `internal/handler/user_follow.go`、`internal/rousters/rousters.go`：写入方只取 Session 中的用户 ID，不接受客户端指定操作者；写操作校验 Origin。
- `frontend/components/follow-button.tsx`：复用关注按钮，登录提示、刷新恢复、提交中禁用、失败后核对状态、切换账号重查。
- `frontend/components/author-space.tsx`、`frontend/app/space/[id]/page.tsx`：公开主页，资料、粉丝/关注/投稿数量、视频分页。
- `frontend/components/author-card.tsx`：视频详情作者卡片，点击作者进入主页。

公开用户查询只选择 `user_id,user_name,avatar_url,bio`，用独立 PublicUser DTO 返回计数。邮箱、角色、密码不会进入响应。公开视频与数量共用 `owner_id + published + ready + approved` 过滤条件，并由 GORM 排除软删除记录。即使作者本人访问公开主页，也看不到自己的草稿。

没有对关注关系加入 Redis 缓存或 Kafka：这一批是一条关系的数据库写入，唯一约束已能解决重复请求。粉丝计数较大时再按实际性能引入缓存或计数表。并发 PUT/DELETE 的最终状态以数据库实际执行顺序为准，不承诺不同浏览器中较早发出的请求一定先完成；页面刷新会查询实际状态。

## 2. 接口

| 方法 | 路径 | 权限和行为 |
| --- | --- | --- |
| GET | `/api/v1/users/:user_id` | 公开资料与关注、粉丝、公开视频数量 |
| GET | `/api/v1/users/:user_id/videos?limit=20&cursor=...` | 公开视频，按 video_id 倒序，limit 1–50 |
| GET | `/api/v1/users/:user_id/follow` | 登录后查询当前账号与该作者的关系 |
| PUT | `/api/v1/users/:user_id/follow` | 登录后关注，无需 JSON 请求体，重复调用仍 200 |
| DELETE | `/api/v1/users/:user_id/follow` | 登录后取消关注，未关注时仍 200 |

关注接口成功的 data 为 `{"following":true,"follower_count":1}`。ID 无效/自关注返回 400，未登录返回 401，不可信浏览器来源返回 403，用户不存在返回 404。ID 在公开资料 JSON 中为字符串。

分页使用上一页 `next_cursor`；`has_more=false` 时结束。它是作者作品列表，尚不是推荐视频 Feed，也没有发出关注动态。

## 3. 部署到 Linux

先完成管理员验收：作者上传 → 管理员拒绝 → 作者修改 → 管理员通过 → 作者发布。两个演示开关保持关闭，参见 `video-review-development.md`。

把本批新增和修改的文件同步到服务器（不要只同步前端）。停止旧后端后在项目根目录按原配置启动，例如：

```bash
cd /home/yujun/projects/live-stream
VIDEO_DEMO_MODE=0 \
VIDEO_UPLOAD_AUTO_APPROVE=0 \
VIDEO_STORAGE_DIR=/home/yujun/projects/live-stream/storage/videos \
go run .
```

如果原来通过 systemd/Docker 管理，则更新该服务并重启，不要额外启动第二份后端。查看日志确认 AutoMigrate 没有报错。数据库只新增表/字段，无需删库重建。

前端另开终端：

```bash
cd /home/yujun/projects/live-stream/frontend
pnpm lint
pnpm exec tsc --noEmit
pnpm build
```

构建成功后重启原前端服务，保留现有端口与 `/api/` 代理。2 核 4G 机器顺序运行检查，避免同时构建多个项目。

在已登录网页右上角点击头像进入自己的主页，或从视频详情点击作者名称。作者 ID 为 2 时的示例地址：`http://119.91.119.208:8081/space/2`。将 2 换成实际作者 ID，不是视频 ID。

## 4. 浏览器验收

用作者 A 和观众 B；建议两个浏览器配置或一个正常窗口加一个隐身窗口，避免共用登录 Cookie。

1. A 准备一条已发布视频以及一条草稿或待审核视频。
2. 未登录打开 A 的主页：资料和已发布视频正常显示，无邮箱，无草稿；点击关注会弹出登录框。
3. B 登录，点击关注 A：变为已关注，粉丝数增加 1。刷新页面、从视频详情进入主页，仍是已关注。
4. B 取消关注，再刷新：状态恢复未关注，粉丝数回到原值。连续点击期间按钮禁用。
5. A 登录自己的主页：显示“管理投稿”，不显示关注自己。直接调用自关注接口返回 400。
6. 作者将公开视频下架或删除，重新请求主页：该视频和公开投稿计数移除；直接访问公开详情/媒体返回 404。已经加载在浏览器中的卡片不会自动消失，需要刷新；本批未实现实时作品变更推送。
7. 访问不存在的作者 ID：显示“用户不存在”，不是空白页面。
8. 发布超过 20 个视频后，主页“加载更多”追加下一页；未公开视频不影响可见结果，跨页不重复，失败可重试。

## 5. Linux 接口验收

先登录观众 B 并保存 Cookie。下面命令交互读取密码，不把密码写进命令历史；替换实际作者 ID。

```bash
BASE=http://127.0.0.1:9000
AUTHOR_ID=2
TEST_DIR=$(mktemp -d)
chmod 700 "$TEST_DIR"
read -r -p '观众 B 邮箱: ' TEST_EMAIL
read -r -s -p '观众 B 密码: ' TEST_PASSWORD
printf '\n'
export TEST_EMAIL TEST_PASSWORD
python3 -c 'import json,os; print(json.dumps({"email":os.environ["TEST_EMAIL"],"password":os.environ["TEST_PASSWORD"]}))' \
  | curl -sS --fail-with-body -c "$TEST_DIR/b.cookies" \
      -H 'Content-Type: application/json' --data-binary @- "$BASE/api/v1/auth/login"
unset TEST_PASSWORD TEST_EMAIL
```

确认登录成功后执行。下面会取消并重新关注 A，用专门测试账号执行，最后停在未关注状态：

```bash
curl -sS --fail-with-body -b "$TEST_DIR/b.cookies" -X DELETE "$BASE/api/v1/users/$AUTHOR_ID/follow"
curl -sS --fail-with-body "$BASE/api/v1/users/$AUTHOR_ID" | python3 -m json.tool

for i in $(seq 1 10); do
  curl -sS --fail-with-body -b "$TEST_DIR/b.cookies" -X PUT "$BASE/api/v1/users/$AUTHOR_ID/follow"
  printf '\n'
done

curl -sS --fail-with-body -b "$TEST_DIR/b.cookies" "$BASE/api/v1/users/$AUTHOR_ID/follow" | python3 -m json.tool
curl -sS --fail-with-body "$BASE/api/v1/users/$AUTHOR_ID/videos?limit=20" | python3 -m json.tool
curl -sS --fail-with-body -b "$TEST_DIR/b.cookies" -X DELETE "$BASE/api/v1/users/$AUTHOR_ID/follow"
curl -sS --fail-with-body -b "$TEST_DIR/b.cookies" -X DELETE "$BASE/api/v1/users/$AUTHOR_ID/follow"
```

预期：10 次关注均成功，在没有其他人操作的测试环境里粉丝数只增加 1；两次取消都成功，状态 false，粉丝数回到原值。在数据库中用真实 B、A 的 ID 查询 `SELECT COUNT(*) FROM user_follows WHERE follower_id = B的ID AND followee_id = A的ID;`，关注时为 1，取消后为 0。

未登录测试：

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X PUT "$BASE/api/v1/users/$AUTHOR_ID/follow"
```

应返回 401。登录为 B 时，即使在请求体或 URL 加 `follower_id=A的ID`，服务端也只会为 B 操作，不能代替 A。

## 6. 验证范围与后续

本地自动检查覆盖：关系唯一索引、重复请求 SQL、伪造操作者无效、自关注/匿名/跨域/不存在用户、公开资料字段、作品分页和可见性 SQL。使用 SQL mock，不代表已在真实 MySQL 上完成并发测试。

实际数据库、Cookie、浏览器播放和上述双账号流程需部署后验收。通过后再补 L3-A 的关注/粉丝列表及剩余入口，然后进入 L3-B：图文、内容事件、Outbox 和关注动态。
