'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { toWebSocketBase } from '@/lib/runtime-config';
import type { ConnectionState, DanmakuMessage } from '@/lib/types';

const MAX_MESSAGES = 500;

export function useLiveRoom(apiBase: string, roomId: number, enabled: boolean) {
  const [messages, setMessages] = useState<DanmakuMessage[]>([]);
  const [connection, setConnection] = useState<ConnectionState>('closed');
  const [onlineCount, setOnlineCount] = useState(0);
  const [likeCount, setLikeCount] = useState(0);
  const [deliveryMode, setDeliveryMode] = useState<'push' | 'sampled'>('push');
  const [error, setError] = useState('');
  const wsRef = useRef<WebSocket | null>(null);
  const lastSeqRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttemptRef = useRef(0);
  const recoveringRef = useRef(false);
  const stoppedRef = useRef(false);

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
    stoppedRef.current = false;
    const connect = () => {
      if (stoppedRef.current) return;
      setConnection('connecting');
      const ws = new WebSocket(`${toWebSocketBase(apiBase)}/api/v1/ws?room_id=${roomId}`);
      wsRef.current = ws;
      ws.onopen = () => {
        reconnectAttemptRef.current = 0;
        setConnection('open'); setError('');
        if (lastSeqRef.current > 0) void recover(lastSeqRef.current);
      };
      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as DanmakuMessage & { after_seq?: number; latest_seq?: number; msg?: string };
          if (message.type === 'danma') {
            const sequence = message.room_seq || 0;
            const previous = lastSeqRef.current;
            append([message]);
            if (sequence && previous && sequence > previous + 1) void recover(previous);
          } else if (message.type === 'system') {
            const subtype = message.data?.sub_type;
            if (subtype === 'room_state') {
              setOnlineCount(Number(message.data?.online_count || 0));
              setLikeCount(Number(message.data?.like_count || 0));
            } else if (subtype === 'delivery_policy') {
              setDeliveryMode(message.data?.delivery_mode === 'sampled' ? 'sampled' : 'push');
            }
          } else if (message.type === 'resync_required') {
            void recover(Number(message.after_seq || lastSeqRef.current));
          } else if (message.type === 'error') setError(message.msg || 'WebSocket 消息处理失败');
        } catch { setError('收到无法解析的 WebSocket 消息'); }
      };
      ws.onerror = () => setConnection('error');
      ws.onclose = () => {
        if (wsRef.current === ws) wsRef.current = null;
        if (stoppedRef.current) { setConnection('closed'); return; }
        setConnection('connecting');
        const delay = Math.min(8000, 500 * 2 ** reconnectAttemptRef.current++);
        reconnectTimerRef.current = setTimeout(connect, delay);
      };
    };
    connect();
    return () => {
      stoppedRef.current = true;
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      wsRef.current?.close(1000, 'page left');
      wsRef.current = null;
    };
  }, [apiBase, append, enabled, recover, roomId]);

  useEffect(() => {
    if (!enabled || deliveryMode !== 'sampled') return;
    const timer = setInterval(() => void recover(lastSeqRef.current), 1000);
    return () => clearInterval(timer);
  }, [deliveryMode, enabled, recover]);

  const send = useCallback((type: 'danma' | 'like', content = '') => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return false;
    wsRef.current.send(JSON.stringify({ type, message_id: crypto.randomUUID(), content }));
    return true;
  }, []);

  return { messages, appendMessages: append, connection, onlineCount, likeCount, deliveryMode, error, send, recover };
}
