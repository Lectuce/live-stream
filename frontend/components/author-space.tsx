'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, Film, LoaderCircle, Play } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { FollowButton } from '@/components/follow-button';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { videoDate, videoDuration } from '@/components/creator-video-ui';
import { api } from '@/lib/api';
import type { PublicUser, VideoPage } from '@/lib/types';

export function AuthorSpace({ userId }: { userId: string }) {
  const { apiBase } = useAuth();
  return <><AppHeader /><SpaceContent key={`${apiBase}:${userId}`} base={apiBase} userId={userId} /></>;
}

function SpaceContent({ base, userId }: { base: string; userId: string }) {
  const [author, setAuthor] = useState<PublicUser | null>(null);
  const [page, setPage] = useState<VideoPage | null>(null);
  const [error, setError] = useState('');
  const [pageError, setPageError] = useState('');
  const [busy, setBusy] = useState(false);
  const [retry, setRetry] = useState(0);
  const pending = useRef(false);
  const pagination = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([api.publicUser(base, userId, controller.signal), api.authorVideos(base, userId, '', controller.signal)])
      .then(([profile, videos]) => {
        if (!controller.signal.aborted) { setAuthor(profile.data); setPage(videos.data); setError(''); }
      }).catch((err: unknown) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '主页读取失败');
      });
    return () => { controller.abort(); pagination.current?.abort(); };
  }, [base, userId, retry]);

  async function loadMore() {
    if (!page?.has_more || pending.current) return;
    pending.current = true;
    setBusy(true);
    setPageError('');
    const controller = new AbortController();
    pagination.current = controller;
    try {
      const { data } = await api.authorVideos(base, userId, page.next_cursor, controller.signal);
      if (!controller.signal.aborted) setPage((previous) => {
        const seen = new Set(previous?.items.map((item) => item.video_id));
        return { ...data, items: [...(previous?.items || []), ...data.items.filter((item) => !seen.has(item.video_id))] };
      });
    } catch (err) {
      if (!controller.signal.aborted) setPageError(err instanceof Error ? err.message : '加载失败');
    } finally {
      pending.current = false;
      if (!controller.signal.aborted) setBusy(false);
    }
  }

  return <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-10">
    <Link href="/video" className="mb-6 inline-flex items-center gap-2 text-sm text-white/50 hover:text-white"><ArrowLeft className="size-4" />浏览视频</Link>
    {error ? <div className="creator-panel p-8"><p role="alert" className="text-red-300">{error}</p><button type="button" className="creator-secondary mt-4" onClick={() => { setError(''); setRetry((value) => value + 1); }}>重新加载</button></div>
      : !author || !page ? <p className="flex items-center gap-2 py-16 text-white/50"><LoaderCircle className="size-5 animate-spin" />正在加载作者主页…</p>
      : <>
        <section className="creator-panel overflow-hidden">
          <div className="h-28 border-b border-white/5 bg-gradient-to-r from-[#ff4d3d]/20 via-[#8f536b]/10 to-[#11151d] sm:h-36" />
          <div className="relative px-5 pb-7 sm:px-8">
            <Avatar className="-mt-9 mb-4 size-20 border-4 border-[#0f1219] text-2xl"><AvatarImage src={author.avatar_url || undefined} /><AvatarFallback>{author.user_name.slice(0, 1)}</AvatarFallback></Avatar>
            <div className="flex flex-wrap items-start justify-between gap-5">
              <div className="min-w-0 flex-1"><h1 className="break-words text-2xl font-bold">{author.user_name}</h1><p className="mt-2 whitespace-pre-wrap break-words text-sm text-white/50">{author.bio || '这位创作者还没有填写简介。'}</p></div>
              <FollowButton userId={author.user_id} onChange={(state) => setAuthor((value) => value ? { ...value, follower_count: state.follower_count } : value)} />
            </div>
            <dl className="mt-6 flex gap-8 text-sm">
              {[[author.video_count, '投稿'], [author.follower_count, '粉丝'], [author.following_count, '关注']].map(([count, label]) => <div key={label}><dt className="text-white/40">{label}</dt><dd className="mt-1 text-lg font-semibold tabular-nums">{count}</dd></div>)}
            </dl>
          </div>
        </section>
        <section className="mt-8">
          <div className="mb-5 flex items-center gap-2 border-b border-white/8 pb-4"><Film className="size-5 text-[#ff6b5e]" /><h2 className="font-semibold">视频投稿</h2><span className="ml-auto text-xs text-white/40">最新投稿在前</span></div>
          {page.items.length === 0 ? <div className="creator-panel py-16 text-center"><Film className="mx-auto mb-3 size-8 text-white/20" /><p className="text-white/50">还没有公开的视频</p></div>
            : <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
              {page.items.map((video) => <Link key={video.video_id} href={`/video/${video.video_id}`} className="group min-w-0 overflow-hidden rounded-xl border border-white/8 bg-[#0f1219] transition hover:border-[#ff4d3d]/40">
                <div className="relative grid aspect-video place-items-center bg-gradient-to-br from-white/5 to-black"><Play className="size-10 text-white/25 transition group-hover:text-[#ff6b5e]" /><span className="absolute bottom-2 right-2 rounded bg-black/70 px-1.5 py-0.5 text-xs tabular-nums">{videoDuration(video.duration_ms)}</span></div>
                <div className="p-4"><h3 className="line-clamp-2 break-words font-medium group-hover:text-[#ff6b5e]">{video.title}</h3><p className="mt-3 text-xs text-white/40">{videoDate(video.published_at || video.created_at)} 发布</p></div>
              </Link>)}
            </div>}
          {pageError && <p role="alert" className="mt-5 text-center text-sm text-red-300">{pageError}</p>}
          <div className="mt-8 flex justify-center">{page.has_more ? <button type="button" className="creator-secondary" disabled={busy} onClick={() => void loadMore()}>{busy && <LoaderCircle className="size-4 animate-spin" />}{busy ? '加载中…' : pageError ? '重试加载' : '加载更多'}</button> : page.items.length > 0 && <p className="text-xs text-white/30">已展示全部投稿</p>}</div>
        </section>
      </>}
  </main>;
}
