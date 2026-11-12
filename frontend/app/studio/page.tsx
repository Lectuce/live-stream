'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { OwnerVideoPage } from '@/lib/types';
import { canPublishVideo, isPublicVideo, videoStatus } from '@/lib/video-status';

function MyVideos({ apiBase }: { apiBase: string }) {
  const [cursor, setCursor] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [result, setResult] = useState<{
    cursor: string; refresh: number; data?: OwnerVideoPage; error?: string;
  } | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void api.myVideos(apiBase, cursor, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) setResult({ cursor, refresh, data });
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setResult({ cursor, refresh, error: error instanceof Error ? error.message : '投稿加载失败' });
    });
    return () => controller.abort();
  }, [apiBase, cursor, refresh]);

  const current = result?.cursor === cursor && result.refresh === refresh ? result : null;
  const page = current?.data;
  return <>
    <div className="mt-6 flex items-center justify-between gap-4">
      <p className="text-sm text-white/50">查看全部投稿，继续上传草稿或发布已通过审核的视频。</p>
      <button type="button" onClick={() => setRefresh(value => value + 1)} disabled={!current} className="shrink-0 rounded-lg border border-white/15 px-4 py-2 text-sm disabled:opacity-40">刷新状态</button>
    </div>
    {!current && <p className="mt-8">正在加载投稿…</p>}
    {current?.error && <p role="alert" className="mt-8 text-red-300">{current.error}</p>}
    {page?.items.length === 0 && <p className="mt-8 text-white/50">这一页暂无投稿，点击“新建投稿”开始上传。</p>}
    <div className="mt-6 space-y-4">
      {page?.items.map(video => <article key={video.video_id} className="flex flex-col justify-between gap-4 rounded-2xl border border-white/10 bg-white/[.03] p-5 sm:flex-row sm:items-center">
        <div className="min-w-0">
          <p className="text-xs text-[#ff8d80]">{videoStatus(video)}</p>
          <h2 className="mt-2 break-words text-lg font-semibold">{video.title}</h2>
          <p className="mt-2 line-clamp-2 break-words text-sm text-white/50">{video.description || '暂无简介'}</p>
          <p className="mt-3 text-xs text-white/35">投稿 #{video.video_id}</p>
        </div>
        <div className="flex shrink-0 flex-wrap gap-3">
          <Link href={`/upload?video_id=${encodeURIComponent(video.video_id)}`} className="rounded-lg border border-white/15 px-4 py-2 text-sm">
            {canPublishVideo(video) ? '继续发布' : !video.has_media && video.publish_status === 'draft' ? '继续投稿' : '查看投稿'}
          </Link>
          {isPublicVideo(video) && <Link href={`/video/${video.video_id}`} className="rounded-lg bg-[#ff4d3d] px-4 py-2 text-sm">观看视频</Link>}
        </div>
      </article>)}
    </div>
    <div className="mt-6 flex gap-4">
      {cursor && <button type="button" onClick={() => setCursor('')}>返回第一页</button>}
      {page?.has_more && <button type="button" onClick={() => setCursor(page.next_cursor)}>下一页</button>}
    </div>
  </>;
}

export default function StudioPage() {
  const { user, loading, apiBase } = useAuth();
  return <>
    <AppHeader />
    <main className="mx-auto max-w-5xl px-4 py-10">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-3xl font-bold">创作中心</h1>
        <Link href="/upload" className="rounded-lg bg-[#ff4d3d] px-4 py-2 text-sm font-semibold">新建投稿</Link>
      </div>
      {loading ? <p className="mt-8">正在检查登录状态…</p> : !user ? <p className="mt-8">请先点击右上角登录，查看自己的投稿。</p> : <MyVideos key={`${apiBase}:${user.user_id}`} apiBase={apiBase} />}
    </main>
  </>;
}
