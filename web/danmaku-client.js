import { DanmakuRecovery } from "./danmaku-recovery.js";
// 浏览器参考客户端：优先使用推送，并通过自动拉取恢复数据。
// 它会使用指数退避重连 WebSocket、按 message_id 去重、按 room_seq 补拉缺口，
// 并在热门房间处于采样模式时轮询由 Redis 支持的近期消息重放接口。
export class DanmakuClient {
  constructor(options) {
    this.wsUrl = options.wsUrl;
    this.apiBase = options.apiBase.replace(/\/$/, "");
    this.roomId = Number(options.roomId);
    this.onDanmaku = options.onDanmaku ?? (() => {});
    this.onState = options.onState ?? (() => {});
    this.onMode = options.onMode ?? (() => {});
    this.onError = options.onError ?? console.error;
    this.storageKey = options.storageKey ?? `danmaku:last-seq:${this.roomId}`;
    this.reconnectMinMs = options.reconnectMinMs ?? 250;
    this.reconnectMaxMs = options.reconnectMaxMs ?? 10000;
    this.pollIntervalMs = options.pollIntervalMs ?? 4000;
 this.recovery = null;

    this.socket = null;
    this.stopped = true;
    this.reconnectAttempt = 0;
    this.reconnectTimer = null;
    this.pollTimer = null;
    this.deliveryMode = "push";
    this.lastSeq = Number(localStorage.getItem(this.storageKey) ?? 0);
    this.pullCursor = this.lastSeq;
    this.recovering = null;
    this.liveBuffer = [];
    this.seen = new Set();
    this.seenOrder = [];
  }

  start() {
    if (!this.stopped) return;
    this.stopped = false;
    this.recovery = new DanmakuRecovery(async (cursor, signal) => {
      const endpoint = new URL(`${this.apiBase}/api/v1/rooms/${this.roomId}/messages/replay`);
      endpoint.searchParams.set("after_seq", String(cursor)); endpoint.searchParams.set("limit", "200");
      const response = await fetch(endpoint, {credentials:"include",signal});
      if (!response.ok) { const error=new Error(`replay failed: HTTP ${response.status}`); error.retryAfterMilliseconds=Number(response.headers.get("Retry-After")||0)*1000; throw error; }
      return (await response.json()).data;
    }, items => { for (const item of items) this.#deliver(item); }, this.onError);
    this.#connect();
  }

  stop() {
    this.stopped = true;
    this.recovery?.stop();
    clearTimeout(this.reconnectTimer);
    clearTimeout(this.pollTimer);
    this.reconnectTimer = null;
    this.pollTimer = null;
    if (this.socket) {
      this.socket.onclose = null;
      this.socket.close(1000, "client stopped");
      this.socket = null;
    }
  }

  send(content, extra = undefined) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket is not connected");
    }
    const messageId = crypto.randomUUID();
    this.socket.send(JSON.stringify({
      type: "danma",
      message_id: messageId,
      content,
      data: extra,
    }));
    return messageId;
  }

  #connect() {
    if (this.stopped) return;
    const endpoint = new URL(this.wsUrl);
    endpoint.searchParams.set("room_id", String(this.roomId));
    const socket = new WebSocket(endpoint);
    this.socket = socket;

    socket.onopen = async () => {
      this.reconnectAttempt = 0;
      try {
        // 套接字断开期间可能已有新消息发布。
        if (this.lastSeq > 0) await this.#recover(this.lastSeq, 0);
      } catch (error) {
        this.onError(error);
      }
    };
    socket.onmessage = (event) => this.#handleFrame(event.data);
    socket.onerror = () => socket.close();
    socket.onclose = () => {
      if (this.socket === socket) this.socket = null;
      if (!this.stopped) this.#scheduleReconnect();
    };
  }

  #scheduleReconnect() {
    const base = Math.min(
      this.reconnectMaxMs,
      this.reconnectMinMs * 2 ** this.reconnectAttempt,
    );
    this.reconnectAttempt += 1;
    const delay = Math.round(base * (0.75 + Math.random() * 0.5));
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = setTimeout(() => this.#connect(), delay);
  }

  #handleFrame(raw) {
    let message;
    try {
      message = JSON.parse(raw);
    } catch (error) {
      this.onError(error);
      return;
    }

    if (message.type === "resync_required") {
      this.#recover(
        Number(message.after_seq ?? this.lastSeq),
        Number(message.latest_seq ?? 0),
      ).catch(this.onError);
      return;
    }

    if (message.type === "system" && message.data?.sub_type === "delivery_policy") {
      this.#setDeliveryMode(
        message.data.delivery_mode ?? "push",
        Number(message.data.poll_interval_ms ?? this.pollIntervalMs),
        this.lastSeq > 0 ? Number(message.data.replay_after_seq ?? this.lastSeq) : Number(message.room_seq ?? 0),
      );
      this.onState(message);
      return;
    }

    if (message.type === "danma" || message.content) {
      this.#deliver(message);
      return;
    }
    this.onState(message);
  }

  #setDeliveryMode(mode, pollIntervalMs, replayAfter) {
    this.deliveryMode = mode;
    this.onMode(mode);
    this.recovery?.mode(mode === "sampled", this.lastSeq > 0 ? Math.min(this.lastSeq, replayAfter) : replayAfter);
  }

  async #recover(afterSeq, targetSeq) {
    this.recovery?.request(afterSeq, targetSeq);
    localStorage.setItem(this.storageKey, String(this.recovery?.checkpoint ?? this.lastSeq));
  }

  #deliver(message) {
    const messageId = message.message_id;
    if (messageId && this.seen.has(messageId)) return;
    if (messageId) {
      this.seen.add(messageId);
      this.seenOrder.push(messageId);
      if (this.seenOrder.length > 10000) {
        this.seen.delete(this.seenOrder.shift());
      }
    }
    const sequence = Number(message.room_seq ?? 0);
    if (sequence > this.lastSeq) {
      this.lastSeq = sequence;
      localStorage.setItem(this.storageKey, String(this.recovery?.checkpoint ?? sequence));
    }
    this.onDanmaku(message);
  }
}
