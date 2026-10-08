'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { toWebSocketBase } from '@/lib/runtime-config';
import type { ConnectionState, DanmakuMessage } from '@/lib/types';

const MAX_MESSAGES = 500;
const CONNECTION_TIMEOUT_MS = 8000;
const STABLE_CONNECTION_MS = 10000;
const LIVE_STREAM_SUBPROTOCOL = 'vite-live-stream-v1';

type LikeSendState = 'idle' | 'accepted' | 'error';

function describeWebSocketClose(event: CloseEvent) {
  switch (event.code) {
    case 1000:
      return '直播互动已结束';
    case 1001:
      return '直播互动暂时关闭';
    case 1006:
      return '直播互动意外中断';
    case 4001:
      return event.reason === 'control_queue_full'
        ? '当前互动人数较多，连接已暂时中断'
        : '消息同步繁忙，正在重新加入';
    default:
      return '直播互动已中断';
  }
}

function createMessageID() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  const bytes = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues) globalThis.crypto.getRandomValues(bytes);
  else for (let index = 0; index < bytes.length; index++) bytes[index] = Math.floor(Math.random() * 256);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function useLiveRoom(apiBase: string, roomId: number, enabled: boolean, initialLikeCount = 0) {
  const [messages, setMessages] = useState<DanmakuMessage[]>([]);
  const [connection, setConnection] = useState<ConnectionState>('closed');
  const [onlineCount, setOnlineCount] = useState(0);
  const [likeCount, setLikeCount] = useState(0);
  const [deliveryMode, setDeliveryMode] = useState<'push' | 'sampled'>('push');
  const [error, setError] = useState('');
  const [optimisticLikeCount, setOptimisticLikeCount] = useState(0);
  const [likeSendState, setLikeSendState] = useState<LikeSendState>('idle');
  const wsRef = useRef<WebSocket | null>(null);
  const lastSeqRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const connectionTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stableConnectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttemptRef = useRef(0);
  const recoveringRef = useRef(false);
  const pendingLikesRef = useRef(new Map<string, boolean>());
  const pendingLikeTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const serverLikeCountRef = useRef(Math.max(0, initialLikeCount));
  const likeFeedbackTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showLikeState = useCallback((state: LikeSendState, resetAfter = 0) => {
    if (likeFeedbackTimerRef.current) clearTimeout(likeFeedbackTimerRef.current);
    setLikeSendState(state);
    if (resetAfter > 0) {
      likeFeedbackTimerRef.current = setTimeout(() => {
        setLikeSendState('idle');
        likeFeedbackTimerRef.current = null;
      }, resetAfter);
    }
  }, []);

  const removePendingLike = useCallback((messageID: string) => {
    const timer = pendingLikeTimersRef.current.get(messageID);
    if (timer) clearTimeout(timer);
    pendingLikeTimersRef.current.delete(messageID);
    if (!pendingLikesRef.current.delete(messageID)) return false;
    setOptimisticLikeCount((count) => Math.max(0, count - 1));
    return true;
  }, []);

  const applyLikeCount = useCallback((nextCount: number) => {
    const normalized = Math.max(0, nextCount);
    const increase = Math.max(0, normalized - serverLikeCountRef.current);
    serverLikeCountRef.current = normalized;
    setLikeCount(normalized);
    if (increase === 0 || pendingLikesRef.current.size === 0) return;

    let reconciled = 0;
    for (const messageID of pendingLikesRef.current.keys()) {
      const timer = pendingLikeTimersRef.current.get(messageID);
      if (timer) clearTimeout(timer);
      pendingLikeTimersRef.current.delete(messageID);
      pendingLikesRef.current.delete(messageID);
      reconciled++;
      if (reconciled >= increase) break;
    }
    if (reconciled > 0) {
      setOptimisticLikeCount((count) => Math.max(0, count - reconciled));
      showLikeState('accepted', 1200);
    }
  }, [showLikeState]);

  const append = useCallback((incoming: DanmakuMessage[]) => {
    if (!incoming.length) return;
    for (const item of incoming) if (item.room_seq) lastSeqRef.current = Math.max(lastSeqRef.current, item.room_seq);
    setMessages((current) => {
      const byID = new Map(current.map((item) => [item.message_id || `${item.room_seq}-${item.id}`, item]));
      incoming.forEach((item) => byID.set(item.message_id || `${item.room_seq}-${item.id}`, item));
      return Array.from(byID.values()).sort((a, b) => (a.room_seq || 0) - (b.room_seq || 0)).slice(-MAX_MESSAGES);
    });
  }, []);

  const recover = useCallback(async (afterSeq = lastSeqRef.current) => {
    if (recoveringRef.current || !roomId) return;
    // 补拉是已建立 WebSocket 后的后台同步，不能把可发送的连接临时标成
    // recovering。热点房间每秒补拉一次，若在这里切换 connection，依赖
    // connection 的点赞和弹幕按钮就会每秒禁用/启用一次，造成持续闪烁。
    recoveringRef.current = true;
    try {
      let cursor = afterSeq;
      for (let page = 0; page < 20; page++) {
        const result = (await api.replay(apiBase, roomId, cursor, 200)).data;
        append(result.items || []);
        cursor = Math.max(cursor, result.next_seq || 0);
        if (!result.has_more) break;
      }
      // 重放成功只能清除先前的重放失败，不能清除 WebSocket 断开错误，
      // 否则每秒执行一次的采样重放循环会让连接警告反复闪烁。
      setError((current) => current === '消息同步失败，请稍后重试' ? '' : current);
    } catch { setError('消息同步失败，请稍后重试'); }
    finally { recoveringRef.current = false; }
  }, [apiBase, append, roomId]);

  // 消息和恢复回调可能随房间状态变化，但这些变化不应断开健康的套接字。
  // 连接副作用应只依赖于标识连接本身的值。
  const appendRef = useRef(append);
  const applyLikeCountRef = useRef(applyLikeCount);
  const recoverRef = useRef(recover);
  const removePendingLikeRef = useRef(removePendingLike);
  const showLikeStateRef = useRef(showLikeState);
  useEffect(() => {
    appendRef.current = append;
    applyLikeCountRef.current = applyLikeCount;
    recoverRef.current = recover;
    removePendingLikeRef.current = removePendingLike;
    showLikeStateRef.current = showLikeState;
  }, [append, applyLikeCount, recover, removePendingLike, showLikeState]);

  useEffect(() => {
    if (!enabled || !roomId) return;
    let disposed = false;
    const connect = () => {
      if (disposed) return;
      const current = wsRef.current;
      if (current && (current.readyState === WebSocket.CONNECTING || current.readyState === WebSocket.OPEN)) return;
      setConnection('connecting');
      // Cloudflare 开发插件和 Vite 的 /api 代理都会监听 HTTP 升级请求。
      // 此应用子协议会让 Cloudflare 将套接字交给 Vite 代理，
      // 而不是破坏已代理的数据流。
      const ws = new WebSocket(
        `${toWebSocketBase(apiBase)}/api/v1/ws?room_id=${roomId}`,
        LIVE_STREAM_SUBPROTOCOL,
      );
      wsRef.current = ws;
      connectionTimeoutRef.current = setTimeout(() => {
        if (disposed || wsRef.current !== ws || ws.readyState !== WebSocket.CONNECTING) return;
        setConnection('error');
        setError('加入直播互动超时，请稍后重试');
        ws.close();
      }, CONNECTION_TIMEOUT_MS);
      ws.onopen = () => {
        if (disposed || wsRef.current !== ws) return;
        if (connectionTimeoutRef.current) clearTimeout(connectionTimeoutRef.current);
        connectionTimeoutRef.current = null;
        setConnection('open'); setError('');
        if (stableConnectionTimerRef.current) clearTimeout(stableConnectionTimerRef.current);
        stableConnectionTimerRef.current = setTimeout(() => {
          if (!disposed && wsRef.current === ws && ws.readyState === WebSocket.OPEN) {
            reconnectAttemptRef.current = 0;
          }
          stableConnectionTimerRef.current = null;
        }, STABLE_CONNECTION_MS);
        if (lastSeqRef.current > 0) void recoverRef.current(lastSeqRef.current);
      };
      ws.onmessage = (event) => {
        if (disposed || wsRef.current !== ws) return;
        try {
          const message = JSON.parse(event.data) as DanmakuMessage & { after_seq?: number; latest_seq?: number; msg?: string };
          if (message.type === 'danma') {
            appendRef.current([message]);
          } else if (message.type === 'system') {
            const subtype = message.data?.sub_type;
            if (subtype === 'room_state') {
              setOnlineCount(Number(message.data?.online_count || 0));
              applyLikeCountRef.current(Number(message.data?.like_count || 0));
            } else if (subtype === 'delivery_policy') {
              setDeliveryMode(message.data?.delivery_mode === 'sampled' ? 'sampled' : 'push');
            }
          } else if (message.type === 'resync_required') {
            void recoverRef.current(Number(message.after_seq || lastSeqRef.current));
          } else if (message.type === 'accepted') {
            if (message.message_id && pendingLikesRef.current.has(message.message_id)) {
              pendingLikesRef.current.set(message.message_id, true);
              const timer = pendingLikeTimersRef.current.get(message.message_id);
              if (timer) clearTimeout(timer);
              pendingLikeTimersRef.current.delete(message.message_id);
              showLikeStateRef.current('accepted', 700);
            }
          } else if (message.type === 'error') {
            if (message.message_id && removePendingLikeRef.current(message.message_id)) showLikeStateRef.current('error', 2000);
            setError(message.msg || '消息处理失败，请稍后重试');
          }
        } catch { setError('收到一条异常消息，请稍后重试'); }
      };
      ws.onerror = () => {
        if (disposed || wsRef.current !== ws) return;
        setConnection('error');
        setError('暂时无法加入直播互动，请稍后重试');
      };
      ws.onclose = (event) => {
        if (disposed || wsRef.current !== ws) return;
        if (connectionTimeoutRef.current) clearTimeout(connectionTimeoutRef.current);
        connectionTimeoutRef.current = null;
        if (stableConnectionTimerRef.current) clearTimeout(stableConnectionTimerRef.current);
        stableConnectionTimerRef.current = null;
        wsRef.current = null;
        if (event.code === 1000 && event.reason === 'page left') {
          setConnection('closed');
          return;
        }
        let rolledBack = 0;
        for (const [messageID, accepted] of pendingLikesRef.current) {
          if (accepted) continue;
          const timer = pendingLikeTimersRef.current.get(messageID);
          if (timer) clearTimeout(timer);
          pendingLikeTimersRef.current.delete(messageID);
          pendingLikesRef.current.delete(messageID);
          rolledBack++;
        }
        if (rolledBack > 0) {
          setOptimisticLikeCount((count) => Math.max(0, count - rolledBack));
          showLikeStateRef.current('error', 2000);
        }
        setConnection('error');
        const closeMessage = describeWebSocketClose(event);
        setError(rolledBack > 0
          ? `${closeMessage}；未确认的点赞未计入，正在尝试重连`
          : `${closeMessage}，正在尝试重连`);
        const delay = Math.min(8000, 500 * 2 ** reconnectAttemptRef.current++);
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null;
          connect();
        }, delay);
      };
    };
    connect();
    return () => {
      disposed = true;
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
      if (connectionTimeoutRef.current) clearTimeout(connectionTimeoutRef.current);
      connectionTimeoutRef.current = null;
      if (stableConnectionTimerRef.current) clearTimeout(stableConnectionTimerRef.current);
      stableConnectionTimerRef.current = null;
      const ws = wsRef.current;
      wsRef.current = null;
      if (ws) {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        ws.close(1000, 'page left');
      }
    };
  }, [apiBase, enabled, roomId]);

  useEffect(() => {
    if (!enabled || deliveryMode !== 'sampled') return;
    const timer = setInterval(() => void recover(lastSeqRef.current), 1000);
    return () => clearInterval(timer);
  }, [deliveryMode, enabled, recover]);

  const send = useCallback((type: 'danma' | 'like', content = '') => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      setError('互动尚未准备好，请稍后再试');
      if (type === 'like') showLikeState('error', 2000);
      return false;
    }
    const messageID = createMessageID();
    if (type === 'like') {
      pendingLikesRef.current.set(messageID, false);
      setOptimisticLikeCount((count) => count + 1);
      // 连续点赞采用即时反馈；accepted/error 仍用于最终确认或回滚。
      showLikeState('accepted', 700);
    }
    try {
      wsRef.current.send(JSON.stringify({ type, message_id: messageID, content }));
      if (type === 'like') {
        const timer = setTimeout(() => {
          pendingLikeTimersRef.current.delete(messageID);
          if (!removePendingLike(messageID)) return;
          showLikeState('error', 2000);
          setError('点赞确认超时，请稍后重试');
        }, 8000);
        pendingLikeTimersRef.current.set(messageID, timer);
      }
      setError('');
      return true;
    } catch (sendError) {
      if (type === 'like') {
        removePendingLike(messageID);
        showLikeState('error', 2000);
      }
      setError(sendError instanceof Error ? `消息发送失败：${sendError.message}` : '消息发送失败');
      return false;
    }
  }, [removePendingLike, showLikeState]);

  useEffect(() => {
    const normalized = Math.max(0, initialLikeCount);
    if (normalized > serverLikeCountRef.current) serverLikeCountRef.current = normalized;
  }, [initialLikeCount]);

  useEffect(() => {
    const pendingLikeTimers = pendingLikeTimersRef.current;
    return () => {
      if (likeFeedbackTimerRef.current) clearTimeout(likeFeedbackTimerRef.current);
      for (const timer of pendingLikeTimers.values()) clearTimeout(timer);
      pendingLikeTimers.clear();
    };
  }, []);

  const displayedLikeCount = Math.max(likeCount, initialLikeCount) + optimisticLikeCount;
  return { messages, appendMessages: append, connection, onlineCount, likeCount: displayedLikeCount, deliveryMode, error, send, recover, likeSendState };
}
