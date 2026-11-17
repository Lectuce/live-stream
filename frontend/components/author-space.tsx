'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import {
  ArrowLeft,
  ArrowUpRight,
  Bell,
  Bookmark,
  Clock3,
  FileText,
  Film,
  LayoutDashboard,
  LoaderCircle,
  PenLine,
  Play,
  RadioTower,
  Sparkles,
  Upload,
  type LucideIcon,
} from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { FollowButton } from '@/components/follow-button';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { videoDate, videoDuration } from '@/components/creator-video-ui';
import { api } from '@/lib/api';
import { PostsList } from '@/components/posts-list';
import type { PublicUser, VideoPage } from '@/lib/types';

export function AuthorSpace({ userId }: { userId: string }) {
  const { apiBase, user, loading } = useAuth();
  return <><AppHeader /><SpaceContent key={`${apiBase}:${userId}:${user?.user_id ?? 'guest'}`} base={apiBase} userId={userId} viewerId={user ? String(user.user_id) : ''} authLoading={loading} /></>;
}

function SpaceContent({ base, userId, viewerId, authLoading }: { base: string; userId: string; viewerId: string; authLoading: boolean }) {
  const [author, setAuthor] = useState<PublicUser | null>(null);
  const [page, setPage] = useState<VideoPage | null>(null);
  const [error, setError] = useState('');
  const [pageError, setPageError] = useState('');
  const [busy, setBusy] = useState(false);
  const [retry, setRetry] = useState(0);
  const pending = useRef(false);
  const pagination = useRef<AbortController | null>(null);
  const isOwner = !!viewerId && viewerId === author?.user_id;

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
              {!authLoading && (isOwner ? <span className="rounded-full border border-[#ff786c]/20 bg-[#ff4d3d]/10 px-4 py-2 text-xs font-semibold text-[#ff8a80]">我的主页</span> : <FollowButton userId={author.user_id} onChange={(state) => setAuthor((value) => value ? { ...value, follower_count: state.follower_count } : value)} />)}
            </div>
            <dl className="mt-6 flex gap-8 text-sm">
              {[[author.video_count, '投稿'], [author.follower_count, '粉丝'], [author.following_count, '关注']].map(([count, label]) => <div key={label}><dt className="text-white/40">{label}</dt><dd className="mt-1 text-lg font-semibold tabular-nums">{count}</dd></div>)}
            </dl>
          </div>
        </section>
        {isOwner && <PersonalHub />}
        <section className="mt-8"><h2 className="mb-5 border-b border-white/8 pb-4 font-semibold">图文投稿</h2><PostsList author={author.user_id} /></section>
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

const personalLinks: Array<{ href: string; title: string; description: string; icon: LucideIcon }> = [
  { href: '/studio', title: '投稿中心', description: '管理视频草稿、审核和发布状态', icon: LayoutDashboard },
  { href: '/posts', title: '我的图文', description: '管理图文内容和草稿', icon: FileText },
  { href: '/posts/new', title: '发布图文', description: '分享文字、图片和创作近况', icon: PenLine },
  { href: '/dynamic', title: '关注动态', description: '查看关注作者的最新内容', icon: Sparkles },
  { href: '/live#studio', title: '直播中心', description: '创建房间并获取推流信息', icon: RadioTower },
  { href: '/me/favorites', title: '我的收藏', description: '回看收藏过的视频', icon: Bookmark },
  { href: '/me/history', title: '观看历史', description: '继续上次没有看完的内容', icon: Clock3 },
  { href: '/messages', title: '消息中心', description: '查看回复、关注和审核通知', icon: Bell },
];

function PersonalHub() {
  return <section className="creator-panel mt-6 p-5 sm:p-7" aria-label="个人中心">
    <div className="flex flex-wrap items-end justify-between gap-4 border-b border-white/8 pb-5">
      <div><p className="text-[10px] font-semibold tracking-[.18em] text-[#ff786c]">PERSONAL CENTER</p><h2 className="mt-2 text-xl font-bold">我的空间</h2><p className="mt-2 text-sm text-white/40">创作、互动和观看记录都可以从这里进入。</p></div>
      <Link href="/upload" className="creator-primary"><Upload className="size-4" />发布视频</Link>
    </div>
    <div className="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {personalLinks.map(({ href, title, description, icon: Icon }) => <Link key={href} href={href} className="group relative min-h-28 rounded-xl border border-white/8 bg-white/[.025] p-4 transition hover:-translate-y-0.5 hover:border-[#ff786c]/30 hover:bg-white/[.045]">
        <div className="mb-4 grid size-9 place-items-center rounded-lg bg-white/5 text-white/55 transition group-hover:bg-[#ff4d3d]/10 group-hover:text-[#ff8a80]"><Icon className="size-4" /></div>
        <h3 className="text-sm font-semibold">{title}</h3><p className="mt-1.5 pr-5 text-xs leading-5 text-white/35">{description}</p><ArrowUpRight className="absolute right-4 top-4 size-3.5 text-white/20 transition group-hover:text-[#ff8a80]" />
      </Link>)}
    </div>
  </section>;
}
