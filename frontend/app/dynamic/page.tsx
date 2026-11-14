'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { LoaderCircle, Plus, RefreshCw, Play, Rss } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { CreatorAccess } from '@/components/creator-shell';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { PostBody } from '@/components/post-card';
import { videoDate, videoDuration } from '@/components/creator-video-ui';
import { api } from '@/lib/api';
import type { FollowingFeedItem, FollowingFeedPage } from '@/lib/types';

export default function DynamicPage() {
  const { user, loading, apiBase } = useAuth();
  return <><AppHeader /><main className="mx-auto max-w-4xl px-4 py-8 sm:px-6">
    <div className="mb-8 flex flex-wrap items-end justify-between gap-4"><div><p className="mb-2 text-xs tracking-widest text-[#ff786c]">FOLLOWING</p><h1 className="text-3xl font-bold">关注动态</h1><p className="mt-3 text-sm text-white/45">你关注的人，最近分享了什么</p></div><div className="flex gap-2"><Link href="/posts" className="creator-secondary">我的图文</Link><Link href="/posts/new" className="creator-primary"><Plus className="size-4" />发图文</Link></div></div>
    {loading || !user ? <CreatorAccess loading={loading} /> : <Timeline key={`${apiBase}:${user.user_id}`} base={apiBase} />}
  </main></>;
}

function Timeline({ base }: { base: string }) {
  const [items, setItems] = useState<FollowingFeedItem[]>([]);
  const [page, setPage] = useState<FollowingFeedPage | null>(null);
  const [cursor, setCursor] = useState('');
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void api.followingFeed(base, cursor, controller.signal).then(({ data }) => {
      if (controller.signal.aborted) return;
      setPage(data); setError('');
      setItems((previous) => {
        if (!cursor) return data.items;
        const seen = new Set(previous.map((item) => item.event_id));
        return [...previous, ...data.items.filter((item) => !seen.has(item.event_id))];
      });
    }).catch((err: unknown) => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '动态加载失败'); })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
  }, [base, cursor, revision]);

  return <>
    <div className="mb-4 flex items-center justify-between border-b border-white/8 pb-4"><span className="text-sm text-white/60">全部动态 · 最新在前</span><button type="button" className="flex items-center gap-2 text-xs text-white/50 hover:text-white" onClick={() => { setBusy(true); setCursor(''); setRevision((value) => value + 1); }}><RefreshCw className={`size-3.5 ${busy ? 'animate-spin' : ''}`} />刷新</button></div>
    <div className="space-y-5">{items.map((item) => <article key={item.event_id} className="creator-panel p-5 sm:p-7">
      <Link href={`/space/${item.author.user_id}`} className="mb-5 flex w-fit items-center gap-3"><Avatar><AvatarImage src={item.author.avatar_url || undefined} /><AvatarFallback>{item.author.user_name.slice(0, 1)}</AvatarFallback></Avatar><div><p className="text-sm font-semibold hover:text-[#ff786c]">{item.author.user_name}</p><p className="mt-1 text-xs text-white/35">{videoDate(item.published_at)} · {item.type === 'video.published' ? '投稿了视频' : '发布了图文'}</p></div></Link>
      {item.video && <Link href={`/video/${item.video.video_id}`} className="group flex flex-col overflow-hidden rounded-xl border border-white/8 bg-white/[.025] sm:flex-row"><div className="relative grid aspect-video shrink-0 place-items-center bg-black/35 sm:w-60"><Play className="size-8 text-white/30 group-hover:text-[#ff786c]" /><span className="absolute right-2 bottom-2 rounded bg-black/70 px-1 text-xs">{videoDuration(item.video.duration_ms)}</span></div><div className="p-5"><h2 className="break-words font-semibold group-hover:text-[#ff786c]">{item.video.title}</h2><p className="mt-3 text-xs text-white/40">点击观看视频</p></div></Link>}
      {item.post && <><PostBody post={item.post} compact /><Link href={`/posts/${item.post.post_id}`} className="mt-4 inline-block text-xs text-[#ff786c]">查看图文详情 →</Link></>}
    </article>)}</div>
    {!busy && !error && items.length === 0 && <div className="creator-panel py-16 text-center"><Rss className="mx-auto mb-4 size-9 text-white/20" /><h2 className="font-semibold">还没有动态</h2><p className="mt-3 text-sm text-white/40">关注感兴趣的作者后，在这里查看他们的公开投稿。</p><Link href="/video" className="creator-secondary mt-6">去发现创作者</Link></div>}
    {error && <p role="alert" className="mt-5 text-sm text-red-300">{error}</p>}
    <div className="mt-7 flex justify-center">{busy ? <p className="flex items-center gap-2 text-sm text-white/50"><LoaderCircle className="size-4 animate-spin" />正在加载…</p> : error ? <button className="creator-secondary" onClick={() => { setBusy(true); setRevision((value) => value + 1); }}>重试</button> : page?.has_more ? <button className="creator-secondary" onClick={() => { setBusy(true); setCursor(page.next_cursor); }}>加载更多</button> : items.length > 0 && <p className="text-xs text-white/30">暂时没有更多动态了</p>}</div>
  </>;
}
