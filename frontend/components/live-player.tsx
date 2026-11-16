'use client';

import { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';

export function LivePlayer({
  src,
  muted = false,
}: {
  src: string;
  muted?: boolean;
}) {
  const media = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const video = media.current;
    if (!video || !src) return;
    let hls: Hls | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let networkAttempts = 0;
    let mediaAttempts = 0;
    let disposed = false;
    const fail = (detail = '') =>
      setError(
        detail
          ? `暂时无法播放（${detail}），请重新连接。`
          : '暂时无法播放，主播可能正在连接。请稍后重试。',
      );
    const play = () => {
      if (!disposed) {
        setError('');
        void video.play().catch(() => {
          /* Browser may require the play button. */
        });
      }
    };
    video.addEventListener('canplay', play);
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      const nativeError = () => fail('原生 HLS 加载失败');
      video.addEventListener('error', nativeError);
      video.src = src;
      return () => {
        disposed = true;
        video.removeEventListener('canplay', play);
        video.removeEventListener('error', nativeError);
        video.pause();
        video.removeAttribute('src');
        video.load();
      };
    } else if (Hls.isSupported()) {
      hls = new Hls({
        maxBufferLength: 30,
        backBufferLength: 30,
        liveSyncDurationCount: 3,
        liveMaxLatencyDurationCount: 8,
        manifestLoadingMaxRetry: 8,
        levelLoadingMaxRetry: 8,
        fragLoadingMaxRetry: 8,
      });
      hls.loadSource(src);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        networkAttempts = 0;
        setError('');
        void video.play().catch(() => {
          /* The controls remain available when autoplay is blocked. */
        });
      });
      hls.on(Hls.Events.FRAG_LOADED, () => {
        networkAttempts = 0;
      });
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal || disposed) return;

        if (
          data.type === Hls.ErrorTypes.NETWORK_ERROR &&
          ++networkAttempts <= 8
        ) {
          clearTimeout(timer);
          timer = setTimeout(
            () => {
              if (disposed) return;
              // Reloading the source obtains a fresh live playlist after an old
              // fragment was removed from the server's sliding window.
              hls?.loadSource(src);
              hls?.startLoad(-1);
            },
            Math.min(networkAttempts * 1000, 5000),
          );
          return;
        }

        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && ++mediaAttempts <= 3) {
          if (mediaAttempts === 2) hls?.swapAudioCodec();
          hls?.recoverMediaError();
          return;
        }

        hls?.destroy();
        fail(data.details);
      });
    } else
      queueMicrotask(() => {
        if (!disposed) setError('此浏览器不支持 HLS 播放，请使用新版浏览器。');
      });
    return () => {
      disposed = true;
      clearTimeout(timer);
      hls?.destroy();
      video.removeEventListener('canplay', play);
      video.pause();
      video.removeAttribute('src');
      video.load();
    };
  }, [src, retry]);
  return (
    <div className="relative size-full bg-black">
      <video
        ref={media}
        className="size-full object-contain"
        controls
        autoPlay
        muted={muted}
        playsInline
        aria-label="直播画面"
      >
        <track
          kind="captions"
          srcLang="zh"
          label="暂无字幕"
          src="data:text/vtt,WEBVTT"
        />
      </video>
      {error && (
        <div className="absolute inset-0 grid place-content-center gap-4 bg-black/80 p-6 text-center text-sm">
          <p role="alert">{error}</p>
          <button
            className="creator-secondary"
            onClick={() => {
              setError('');
              setRetry((value) => value + 1);
            }}
          >
            重新连接
          </button>
        </div>
      )}
    </div>
  );
}
