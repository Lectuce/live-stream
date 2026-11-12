'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { Video } from '@/lib/types';

export function VideoDetail({ videoId }: { videoId: string }) {
  const { apiBase } = useAuth();
  const [result, setResult] = useState<{
    id: string;
    video?: Video;
    error?: string;
  } | null>(null);
  const [mediaError, setMediaError] = useState('');

  useEffect(() => {
    const controller = new AbortController();

    void api.videoDetail(apiBase, videoId, controller.signal)
      .then(({ data }) => {
        if (!controller.signal.aborted) {
          setResult({ id: videoId, video: data });
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          setResult({
            id: videoId,
            error: error instanceof Error ? error.message : '视频读取失败',
          });
        }
      });

    return () => controller.abort();
  }, [apiBase, videoId]);

  const current = result?.id === videoId ? result : null;
  const video = current?.video;

  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-5xl p-6">
        <Link href="/video" className="text-sm text-white/60">
          返回视频列表
        </Link>

        {!current && <p className="mt-6">正在加载视频…</p>}

        {current?.error && (
          <p className="mt-6 text-red-400">{current.error}</p>
        )}

        {video && (
          <>
            <h1 className="mt-6 text-2xl font-bold">{video.title}</h1>
            <p className="mt-2 text-sm text-white/50">
              作者 #{video.owner_id}
            </p>

            <video
              key={video.video_id}
              src={video.play_url}
              controls
              playsInline
              preload="metadata"
              className="mt-6 aspect-video w-full rounded-xl bg-black"
              onLoadedMetadata={() => setMediaError('')}
              onError={() => setMediaError('视频加载失败，请检查媒体地址和编码')}
            >
              <track
                kind="captions"
                src="data:text/vtt,WEBVTT"
                srcLang="zh"
                label="暂无字幕"
              />
            </video>

            {mediaError && <p className="mt-3 text-red-400">{mediaError}</p>}

            <p className="mt-6 whitespace-pre-wrap text-white/70">
              {video.description}
            </p>
          </>
        )}
      </main>
    </>
  );
}
