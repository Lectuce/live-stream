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
    let attempts = 0;
    let disposed = false;
    const fail = () => setError('暂时无法播放，主播可能正在连接。请稍后重试。');
    const play = () => {
      if (!disposed) {
        setError('');
        void video.play().catch(() => {
          /* Browser may require the play button. */
        });
      }
    };
    video.addEventListener('canplay', play);
    video.addEventListener('error', fail);
    if (video.canPlayType('application/vnd.apple.mpegurl')) video.src = src;
    else if (Hls.isSupported()) {
      hls = new Hls({ maxBufferLength: 15, backBufferLength: 10 });
      hls.loadSource(src);
      hls.attachMedia(video);
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal || disposed) return;
        if (++attempts <= 3 && data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          clearTimeout(timer);
          timer = setTimeout(() => hls?.loadSource(src), attempts * 2000);
        } else {
          hls?.destroy();
          fail();
        }
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
      video.removeEventListener('error', fail);
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
