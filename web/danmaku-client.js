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
    this.pollIntervalMs = options.pollIntervalMs ?? 1000;

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
    this.#connect();
  }

  stop() {
    this.stopped = true;
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
        Number(message.room_seq ?? 0),
      );
      this.onState(message);
      return;
    }

    if (message.type === "danma" || message.content) {
      if (this.recovering) {
        this.liveBuffer.push(message);
      } else {
        this.#deliver(message);
      }
      return;
    }
    this.onState(message);
  }

  #setDeliveryMode(mode, pollIntervalMs, latestSeq) {
    if (mode === this.deliveryMode && mode !== "sampled") return;
    this.deliveryMode = mode;
    this.onMode(mode);
    clearTimeout(this.pollTimer);
    this.pollTimer = null;
    if (mode === "sampled") {
      if (this.lastSeq === 0 && latestSeq > 0) {
        // 首次进入的观众从当前直播进度开始，而不是重放房间的全部历史消息。
        this.pullCursor = latestSeq;
      } else {
        this.pullCursor = Math.min(this.pullCursor || this.lastSeq, this.lastSeq);
      }
      const poll = async () => {
        if (this.stopped || this.deliveryMode !== "sampled") return;
        try {
          await this.#recover(this.pullCursor, 0, true);
        } catch (error) {
          this.onError(error);
        }
        const jitter = Math.round(pollIntervalMs * (0.8 + Math.random() * 0.4));
        this.pollTimer = setTimeout(poll, jitter);
      };
      this.pollTimer = setTimeout(poll, Math.random() * pollIntervalMs);
    } else {
      this.#recover(this.pullCursor, 0, true).catch(this.onError);
    }
  }

  async #recover(afterSeq, targetSeq, fromPullCursor = false) {
    if (this.recovering) return this.recovering;
    this.recovering = this.#runReplay(afterSeq, targetSeq, fromPullCursor)
      .finally(() => {
        this.liveBuffer.sort((a, b) => Number(a.room_seq) - Number(b.room_seq));
        for (const message of this.liveBuffer) this.#deliver(message);
        this.liveBuffer = [];
        this.recovering = null;
      });
    return this.recovering;
  }

  async #runReplay(afterSeq, targetSeq, fromPullCursor) {
    let cursor = Number(afterSeq || 0);
    let emptyRetries = 0;
    for (let page = 0; page < 100; page += 1) {
      const endpoint = new URL(
        `${this.apiBase}/api/v1/rooms/${this.roomId}/messages/replay`,
      );
      endpoint.searchParams.set("after_seq", String(cursor));
      endpoint.searchParams.set("limit", "500");
      const response = await fetch(endpoint, { credentials: "include" });
      if (!response.ok) throw new Error(`replay failed: HTTP ${response.status}`);
      const body = await response.json();
      const data = body.data ?? {};
      for (const message of data.items ?? []) this.#deliver(message);
      if (Number(data.next_seq ?? 0) > cursor) cursor = Number(data.next_seq);
      if (fromPullCursor) this.pullCursor = Math.max(this.pullCursor, cursor);

      if (data.has_more) continue;
      if (!targetSeq || cursor >= targetSeq) return;
      if (emptyRetries >= 20) {
        throw new Error(`replay did not reach target sequence ${targetSeq}`);
      }
      emptyRetries += 1;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    throw new Error("replay exceeded 100 pages");
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
      localStorage.setItem(this.storageKey, String(sequence));
    }
    this.onDanmaku(message);
  }
}
