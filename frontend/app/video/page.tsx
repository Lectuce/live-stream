'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { VideoPage } from '@/lib/types';

export default function VideosPage() {
  const { apiBase } = useAuth();
  const [cursor, setCursor] = useState('');
  const [result, setResult] = useState<{
    cursor: string;
    data?: VideoPage;
    error?: string;
  } | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    void api.videoList(apiBase, cursor, controller.signal)
      .then(({ data }) => {
        if (!controller.signal.aborted) {
          setResult({ cursor, data });
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          setResult({
            cursor,
            error: error instanceof Error ? error.message : '列表加载失败',
          });
        }
      });

    return () => controller.abort();
  }, [apiBase, cursor]);

  const current = result?.cursor === cursor ? result : null;
  const page = current?.data;

  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-6xl p-6">
        <h1 className="text-2xl font-bold">最新视频</h1>

        {!current && <p className="mt-6">正在加载…</p>}
        {current?.error && (
          <p className="mt-6 text-red-400">{current.error}</p>
        )}

        <div className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {page?.items.map((video) => (
            <Link
              key={video.video_id}
              href={`/video/${video.video_id}`}
              className="rounded-xl border border-white/10 bg-white/5 p-5"
            >
              <div className="grid aspect-video place-items-center rounded-lg bg-black">
                播放视频
              </div>
              <h2 className="mt-3 font-semibold">{video.title}</h2>
              <p className="mt-2 text-sm text-white/50">
                作者 #{video.owner_id}
              </p>
            </Link>
          ))}
        </div>

        {page?.items.length === 0 && (
          <p className="mt-6 text-white/50">暂时没有公开视频</p>
        )}

        <div className="mt-6 flex gap-4">
          {cursor && (
            <button type="button" onClick={() => setCursor('')}>
              返回第一页
            </button>
          )}

          {page?.has_more && (
            <button
              type="button"
              onClick={() => setCursor(page.next_cursor)}
            >
              下一页
            </button>
          )}
        </div>
      </main>
    </>
  );
}