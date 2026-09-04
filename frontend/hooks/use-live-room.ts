'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { toWebSocketBase } from '@/lib/runtime-config';
import type { ConnectionState, DanmakuMessage } from '@/lib/types';

const MAX_MESSAGES = 500;

type LikeSendState = 'idle' | 'sending' | 'accepted' | 'error';

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
    recoveringRef.current = true; setConnection((value) => value === 'open' ? 'recovering' : value);
    try {
      let cursor = afterSeq;
      for (let page = 0; page < 20; page++) {
        const result = (await api.replay(apiBase, roomId, cursor, 200)).data;
        append(result.items || []);
        cursor = Math.max(cursor, result.next_seq || 0);
        if (!result.has_more) break;
      }
      setError('');
    } catch (err) { setError(err instanceof Error ? `补拉失败：${err.message}` : '补拉失败'); }
    finally { recoveringRef.current = false; setConnection((value) => value === 'recovering' ? 'open' : value); }
  }, [apiBase, append, roomId]);

  useEffect(() => {
    if (!enabled || !roomId) return;
    let disposed = false;
    const connect = () => {
      if (disposed) return;
      setConnection('connecting');
      const ws = new WebSocket(`${toWebSocketBase(apiBase)}/api/v1/ws?room_id=${roomId}`);
      wsRef.current = ws;
      ws.onopen = () => {
        if (disposed || wsRef.current !== ws) return;
        reconnectAttemptRef.current = 0;
        setConnection('open'); setError('');
        if (lastSeqRef.current > 0) void recover(lastSeqRef.current);
      };
      ws.onmessage = (event) => {
        if (disposed || wsRef.current !== ws) return;
        try {
          const message = JSON.parse(event.data) as DanmakuMessage & { after_seq?: number; latest_seq?: number; msg?: string };
          if (message.type === 'danma') {
            append([message]);
          } else if (message.type === 'system') {
            const subtype = message.data?.sub_type;
            if (subtype === 'room_state') {
              setOnlineCount(Number(message.data?.online_count || 0));
              applyLikeCount(Number(message.data?.like_count || 0));
            } else if (subtype === 'delivery_policy') {
              setDeliveryMode(message.data?.delivery_mode === 'sampled' ? 'sampled' : 'push');
            }
          } else if (message.type === 'resync_required') {
            void recover(Number(message.after_seq || lastSeqRef.current));
          } else if (message.type === 'accepted') {
            if (message.message_id && pendingLikesRef.current.has(message.message_id)) {
              pendingLikesRef.current.set(message.message_id, true);
              const timer = pendingLikeTimersRef.current.get(message.message_id);
              if (timer) clearTimeout(timer);
              pendingLikeTimersRef.current.delete(message.message_id);
              showLikeState('accepted', 1200);
            }
          } else if (message.type === 'error') {
            if (message.message_id && removePendingLike(message.message_id)) showLikeState('error', 2000);
            setError(message.msg || 'WebSocket 消息处理失败');
          }
        } catch { setError('收到无法解析的 WebSocket 消息'); }
      };
      ws.onerror = () => {
        if (!disposed && wsRef.current === ws) setConnection('error');
      };
      ws.onclose = () => {
        if (disposed || wsRef.current !== ws) return;
        wsRef.current = null;
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
          showLikeState('error', 2000);
          setError('实时连接已断开，未确认的点赞未计入');
        }
        setConnection('connecting');
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
  }, [apiBase, append, applyLikeCount, enabled, recover, removePendingLike, roomId, showLikeState]);

  useEffect(() => {
    if (!enabled || deliveryMode !== 'sampled') return;
    const timer = setInterval(() => void recover(lastSeqRef.current), 1000);
    return () => clearInterval(timer);
  }, [deliveryMode, enabled, recover]);

  const send = useCallback((type: 'danma' | 'like', content = '') => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      setError('实时连接尚未建立，请稍后再试');
      if (type === 'like') showLikeState('error', 2000);
      return false;
    }
    const messageID = createMessageID();
    if (type === 'like') {
      pendingLikesRef.current.set(messageID, false);
      setOptimisticLikeCount((count) => count + 1);
      showLikeState('sending');
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
