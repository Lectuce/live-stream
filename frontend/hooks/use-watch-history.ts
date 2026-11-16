'use client';
import { useEffect, useRef, useState, type RefObject } from 'react';
import { useAuth } from '@/components/auth-provider';
import { ApiError, request } from '@/lib/api';
import { danmakuRequestID } from '@/lib/video-danmaku';
import type { WatchProgress } from '@/lib/watch-history';
import {
  WatchProgressBuffer,
  resumePosition,
} from '@/lib/watch-progress-buffer';
import type { ApiEnvelope } from '@/lib/types';

export function useWatchHistory(
  media: RefObject<HTMLVideoElement | null>,
  videoID: string,
  durationMS: number,
) {
  const { apiBase, user } = useAuth();
  const userID = user?.user_id;
  const [resume, setResume] = useState(0);
  const [status, setStatus] = useState('');
  const [revision, setRevision] = useState(0);
  const resumeAction = useRef<() => void>(() => {});

  useEffect(() => {
    const node = media.current;
    if (!node || !userID) return;
    const abort = new AbortController();
    let alive = true,
      played = false,
      stopped = false,
      sending = false;
    let session: WatchProgress | null = null;
    let snapshot: WatchProgress | null = null;
    let claim: {
      session_id: string;
      session_version: string;
      history_epoch: string;
    } | null = null;
    const buffer = new WatchProgressBuffer();
    const path = `/api/v1/me/history/${videoID}`;
    let retryAt = 0;

    async function call<T>(suffix: string, init?: RequestInit) {
      const timeout = new AbortController();
      const cancel = () => timeout.abort();
      abort.signal.addEventListener('abort', cancel, { once: true });
      const timer = setTimeout(cancel, 10000);
      try {
        return await request<T>(apiBase, path + suffix, {
          ...init,
          signal: timeout.signal,
          cache: 'no-store',
        });
      } finally {
        clearTimeout(timer);
        abort.signal.removeEventListener('abort', cancel);
      }
    }
    function fail(error: unknown) {
      if (!alive) return;
      const code = error instanceof ApiError ? error.status : 0;
      stopped = [400, 401, 403, 404, 409].includes(code);
      retryAt = Date.now() + (code === 429 ? 60000 : 15000);
      setStatus(
        code === 409
          ? '其他播放会话已接管，或历史已被删除。本页已停止保存进度。'
          : code === 401
            ? '登录已失效，当前进度未保存。'
            : stopped
              ? '当前进度无法保存，请重新加载视频。'
              : '进度暂未保存，将自动重试。',
      );
    }
    // Open/read never claims ownership or adds a visible history item.
    const initial = call<ApiEnvelope<WatchProgress>>('')
      .then(({ data }) => {
        if (!alive) return;
        snapshot = data;
        if (!played && data.has_history)
          setResume(resumePosition(data.position_ms, data.duration_ms));
      })
      .catch(fail);

    async function pump() {
      if (
        !alive ||
        stopped ||
        sending ||
        !played ||
        !buffer.pending ||
        Date.now() < retryAt
      )
        return;
      sending = true;
      try {
        await initial;
        if (!alive || stopped) return;
        if (!snapshot)
          snapshot = (await call<ApiEnvelope<WatchProgress>>('')).data;
        if (!alive) return;
        if (!session) {
          claim ??= {
            session_id: danmakuRequestID(),
            session_version: snapshot.session_version,
            history_epoch: snapshot.history_epoch,
          };
          const result = await call<ApiEnvelope<WatchProgress>>('/session', {
            method: 'POST',
            body: JSON.stringify(claim),
          });
          if (!alive) return;
          session = result.data;
        }
        const sample = buffer.pending;
        if (!sample || !alive) return;
        await call('/progress', {
          method: 'PUT',
          body: JSON.stringify({
            session_id: session.session_id,
            session_version: session.session_version,
            history_epoch: session.history_epoch,
            ...sample,
          }),
        });
        buffer.acknowledge(sample.seq);
        if (alive) setStatus('');
      } catch (error) {
        fail(error);
      } finally {
        sending = false;
      }
    }
    function sample() {
      if (!played || stopped || !Number.isFinite(node!.currentTime)) return;
      buffer.sample(
        Math.max(0, Math.min(durationMS, Math.floor(node!.currentTime * 1000))),
      );
    }
    function flush() {
      sample();
      void pump();
    }
    function play() {
      played = true;
      setResume(0);
      flush();
    }
    function leave() {
      sample();
      const pending = buffer.pending;
      if (!session || !pending || stopped) return;
      // Best effort only; periodic/pause saves remain the primary mechanism.
      void request(apiBase, path + '/progress', {
        method: 'PUT',
        keepalive: true,
        body: JSON.stringify({
          session_id: session.session_id,
          session_version: session.session_version,
          history_epoch: session.history_epoch,
          ...pending,
        }),
      }).catch(() => {});
    }
    function visibility() {
      if (document.visibilityState === 'hidden') leave();
    }
    let resumeWanted = false;
    function applyResume() {
      if (!resumeWanted || node!.readyState < 1) return;
      resumeWanted = false;
      const position = resumePosition(
        snapshot?.position_ms || 0,
        node!.duration * 1000,
      );
      node!.currentTime = position / 1000;
      setResume(0);
      void node!.play().catch(() => {
        if (alive) setStatus('请点击播放器开始播放');
      });
    }
    resumeAction.current = () => {
      resumeWanted = true;
      applyResume();
    };
    node.addEventListener('loadedmetadata', applyResume);
    node.addEventListener('play', play);
    node.addEventListener('pause', flush);
    node.addEventListener('seeked', flush);
    node.addEventListener('ended', flush);
    window.addEventListener('pagehide', leave);
    document.addEventListener('visibilitychange', visibility);
    const heartbeat = setInterval(() => {
      if (!node.paused) sample();
      void pump();
    }, 15000);
    // Drain a newer queued sample after an in-flight save without flooding seek events.
    const drain = setInterval(() => void pump(), 1500);
    if (!node.paused) play();
    return () => {
      leave();
      alive = false;
      abort.abort();
      clearInterval(heartbeat);
      clearInterval(drain);
      node.removeEventListener('play', play);
      node.removeEventListener('pause', flush);
      node.removeEventListener('seeked', flush);
      node.removeEventListener('ended', flush);
      node.removeEventListener('loadedmetadata', applyResume);
      window.removeEventListener('pagehide', leave);
      document.removeEventListener('visibilitychange', visibility);
      resumeAction.current = () => {};
    };
  }, [apiBase, userID, videoID, durationMS, media, revision]);

  return {
    resume,
    status,
    continueWatching: () => resumeAction.current(),
    dismiss: () => setResume(0),
    retry: () => { setResume(0); setStatus(''); setRevision((v) => v + 1); },
  };
}
