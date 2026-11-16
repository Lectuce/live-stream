'use client';
import { useEffect, type RefObject } from 'react';
import { useAuth } from '@/components/auth-provider';
import { ApiError, request } from '@/lib/api';
import type { ApiEnvelope } from '@/lib/types';

// Analytics is independent from resume position. Only foreground forward
// playback samples qualify; the server applies its own clock/deduplication.
export function useVideoPlayBehavior(
  media: RefObject<HTMLVideoElement | null>,
  videoID: string,
  durationMS: number,
) {
  const { user, apiBase } = useAuth();
  const userID = user?.user_id;
  useEffect(() => {
    const node = media.current;
    if (!node || !userID || durationMS <= 0) return;
    const abort = new AbortController();
    let session = '',
      seq = 0,
      busy = false,
      done = false,
      retryAt = 0;
    let pending: { seq: number; position_ms: number } | null = null;
    const path = `/api/v1/videos/${videoID}/play-sessions`;
    async function pulse(finishing = false) {
      if (
        abort.signal.aborted ||
        busy ||
        done ||
        (node!.paused && !finishing) ||
        node!.seeking ||
        document.visibilityState !== 'visible' ||
        Date.now() < retryAt
      )
        return;
      busy = true;
      const timeout = new AbortController();
      const cancel = () => timeout.abort();
      abort.signal.addEventListener('abort', cancel, { once: true });
      const timer = setTimeout(cancel, 8000);
      try {
        if (!session) {
          const { data } = await request<ApiEnvelope<{ session_id: string }>>(
            apiBase,
            path,
            { method: 'POST', signal: timeout.signal },
          );
          session = data.session_id;
        }
        if (abort.signal.aborted) return;
        pending ??= {
          seq: ++seq,
          position_ms: Math.max(0, Math.min(durationMS, Math.floor(node!.currentTime * 1000))),
        };
        const { data } = await request<ApiEnvelope<{ qualified: boolean }>>(
          apiBase,
          path,
          {
            method: 'PUT',
            signal: timeout.signal,
            body: JSON.stringify({ session_id: session, ...pending }),
          },
        );
        pending = null;
        done = data.qualified;
      } catch (error) {
        const code = error instanceof ApiError ? error.status : 0;
        if ([400, 401, 403, 404, 409].includes(code)) done = true;
        retryAt = Date.now() + (code === 429 ? 60000 : 15000);
      } finally {
        busy = false;
        clearTimeout(timer);
        abort.signal.removeEventListener('abort', cancel);
      }
    }
    const play = () => void pulse();
    const finish = () => {
      if (session) void pulse(true);
    };
    const timer = setInterval(play, 5000);
    node.addEventListener('play', play);
    node.addEventListener('seeked', play);
    node.addEventListener('pause', finish);
    node.addEventListener('ended', finish);
    if (!node.paused) play();
    return () => {
      abort.abort();
      clearInterval(timer);
      node.removeEventListener('play', play);
      node.removeEventListener('seeked', play);
      node.removeEventListener('pause', finish);
      node.removeEventListener('ended', finish);
    };
  }, [apiBase, userID, videoID, durationMS, media]);
}
