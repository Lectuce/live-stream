# 直播互动后端开发指南

## 1. 身份认证

不要简单地给用户模型增加可持久化的明文 `password` 字段。正确做法是：请求中接收明文密码，数据库只保存不可逆的 `password_hash`。

用户模型可以改成：

```go
type User struct {
	ID           uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserName     string    `json:"username" gorm:"size:32;uniqueIndex;not null"`
	PasswordHash string    `json:"-" gorm:"size:255;not null"`
	AvatarURL    string    `json:"avatar_url" gorm:"size:512"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
```

注册请求单独定义 DTO：

```go
type RegisterRequest struct {
	UserName string `json:"username" binding:"required,min=2,max=32"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}
```

处理流程：

```text
客户端提交 password
→ 使用 Argon2id + 随机 salt 计算哈希
→ 数据库保存 password_hash
→ 丢弃明文 password
```

不要使用 MD5、SHA-256 直接哈希密码。OWASP 推荐新系统使用 Argon2id，并为每个密码生成唯一 salt。

- [OWASP 密码存储建议](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [Go Argon2 包](https://pkg.go.dev/golang.org/x/crypto/argon2)

建议提供以下接口：

```http
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/me
```

项目已经使用 Redis，因此可以优先使用服务端 Session，无需急着引入 JWT：

```text
登录成功
→ crypto/rand 生成随机 session token
→ Redis 保存 session:<token-hash> = user_id，并设置过期时间
→ token 通过 HttpOnly Cookie 返回
```

生产环境 Cookie 应设置：

```text
Secure
HttpOnly
SameSite=Lax 或 Strict
Path=/
```

这种方案支持主动退出、封禁用户和立即失效，比自行实现 JWT 刷新机制更容易讲清楚。

- [OWASP Session 管理建议](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)

### WebSocket 鉴权

WebSocket 也必须使用 Session。当前接口允许客户端通过：

```text
/ws?room_id=1&user_id=999
```

自行声明身份，任何人都能伪装其他用户。应该改为：

```text
/ws?room_id=1
```

`user_id` 从 Session 中获取。同时将当前的：

```go
CheckOrigin: func(r *http.Request) bool {
	return true
}
```

改成前端域名白名单。WebSocket 会自动携带 Cookie，因此必须检查 `Origin`，避免跨站 WebSocket 劫持。

- [OWASP WebSocket 安全建议](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html)

## 2. 房间接口

房间模型建议改为：

```go
type Room struct {
	ID        uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerID   uint64     `json:"owner_id" gorm:"index;not null"`
	Title     string     `json:"title" gorm:"size:100;not null"`
	Status    int8       `json:"status" gorm:"index;not null"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	StartedAt *time.Time `json:"started_at"`
}
```

先完成以下接口：

```http
POST   /api/v1/rooms
GET    /api/v1/rooms
GET    /api/v1/rooms/:room_id
PATCH  /api/v1/rooms/:room_id
POST   /api/v1/rooms/:room_id/start
POST   /api/v1/rooms/:room_id/stop
```

权限规则：

- 登录用户才能创建房间。
- `owner_id` 从 Session 获取，不能相信请求参数。
- 只有房主可以修改、开始和结束直播。
- 建立 WebSocket 前检查房间是否存在、是否允许进入。

推荐新增分层：

```text
internal/
  handler/     HTTP 参数与响应
  service/     业务规则和权限
  repo/        MySQL 数据访问
  middleware/  身份认证
  auth/        密码与 Session
```

避免把鉴权、数据库和 HTTP 逻辑全部写进 Gin Handler。

## 3. 历史消息

给弹幕增加时间和查询索引：

```go
type Danma struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	MessageID string    `json:"message_id" gorm:"size:36;uniqueIndex;not null"`
	RoomID    uint64    `json:"room_id" gorm:"index:idx_room_history,priority:1;not null"`
	UserID    uint64    `json:"user_id" gorm:"index;not null"`
	Content   string    `json:"content" gorm:"type:varchar(500);not null"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_room_history,priority:2"`
}
```

查询接口：

```http
GET /api/v1/rooms/:room_id/messages?before_id=12345&limit=50
```

不要使用 `page=10000` 形式的深分页，应使用游标分页：

```sql
SELECT *
FROM danmas
WHERE room_id = ?
  AND id < ?
ORDER BY id DESC
LIMIT 50;
```

响应示例：

```json
{
  "items": [],
  "next_cursor": 12280,
  "has_more": true
}
```

前端进入房间时：

1. 先建立 WebSocket，并缓冲实时消息。
2. 请求最近 50 条历史消息。
3. 按 `message_id` 去重合并。
4. 开始正常展示实时消息。

这样能减少“请求历史消息期间恰好产生新弹幕”造成的消息缺口。

## 4. 调整消息链路

当前代码同时向 Redis 和 Kafka 写入：

```text
WebSocket → Redis
          → Kafka
```

这属于双写，可能出现“用户看到了弹幕，但 Kafka 写入失败，历史记录不存在”。

建议改成一条主链路：

```text
WebSocket
→ 鉴权、房间校验、限流、内容校验
→ Kafka
→ 消费者幂等写入 MySQL
→ Redis Pub/Sub
→ 各应用实例广播给本机 WebSocket
```

`message_id` 的唯一索引负责消费重试时的幂等。Kafka 使用 `room_id` 作为消息 Key，同一房间的消息会进入同一分区，更容易维持房间内顺序。

## 5. 开发顺序

建议按照以下顺序推进：

1. 删除启动时的 `DropTable`，否则每次重启都会清空用户、房间和弹幕。
2. 删除 DSN 日志输出，避免数据库密码进入日志。
3. 完成注册、登录、Redis Session 和鉴权中间件。
4. 完成房间 CRUD 与房主权限。
5. 完成历史消息游标分页。
6. 改造 Kafka 双 Consumer Group：实时组 → Redis → WebSocket，持久化组 → MySQL。
7. 添加登录限流、弹幕频率限制和单条消息长度限制。
8. 为认证、权限、幂等消费和历史分页编写测试。

完成以上内容后，项目将从中间件演示程序发展为具有明确业务闭环的实时直播互动后端。
