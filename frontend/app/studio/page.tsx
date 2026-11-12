'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { ArrowRight, ChevronLeft, ChevronRight, Clapperboard, FileVideo, Film, Plus, RefreshCw, Search, SlidersHorizontal } from 'lucide-react';
import { CreatorAccess, CreatorShell } from '@/components/creator-shell';
import { VideoStatusBadge, videoDate, videoDuration } from '@/components/creator-video-ui';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { OwnerVideoPage } from '@/lib/types';
import { canPublishVideo, isPublicVideo } from '@/lib/video-status';

function MyVideos({ apiBase }: { apiBase: string }) {
  const [cursor, setCursor] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
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
  const filtered = page?.items.filter(video => {
    const matchesStatus = status === 'all' || (status === 'published' ? isPublicVideo(video)
      : status === 'ready' ? canPublishVideo(video) : status === 'pending' ? video.has_media && video.audit_status === 'pending'
      : video.publish_status === 'draft' && !video.has_media);
    return matchesStatus && `${video.title} ${video.video_id}`.toLowerCase().includes(query.trim().toLowerCase());
  }) ?? [];

  function goTo(next: string) { setCursor(next); setQuery(''); setStatus('all'); }

  return <section className="creator-panel mt-7 overflow-hidden" aria-label="我的视频稿件">
    <div className="flex flex-wrap items-center justify-between gap-4 border-b border-white/8 px-5 pt-5 sm:px-6">
      <div className="flex items-center gap-2.5 border-b-2 border-[#ff4d3d] pb-4 text-sm font-semibold"><Film className="size-4 text-[#ff786c]" />视频稿件<span className="rounded-md bg-white/6 px-2 py-0.5 text-xs font-normal text-white/45">{page ? `本页 ${page.items.length}` : '—'}</span></div>
      <button type="button" onClick={() => setRefresh(value => value + 1)} disabled={!current} className="mb-4 inline-flex items-center gap-2 text-xs text-white/45 transition hover:text-white disabled:opacity-40"><RefreshCw className={`size-3.5 ${!current ? 'animate-spin' : ''}`} />刷新状态</button>
    </div>
    <div className="flex flex-col gap-3 border-b border-white/6 bg-white/[.015] px-5 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-6">
      <label className="relative block sm:w-72"><Search className="pointer-events-none absolute left-3 top-3 size-4 text-white/30" /><input aria-label="搜索本页投稿" placeholder="搜索本页标题或稿件 ID" value={query} onChange={event => setQuery(event.target.value)} className="creator-input py-2.5 pl-9" /></label>
      <label className="flex items-center gap-2 text-xs text-white/40"><SlidersHorizontal className="size-3.5" /><span>本页状态</span><select value={status} onChange={event => setStatus(event.target.value)} className="rounded-lg border border-white/10 bg-[#0c0f15] px-3 py-2.5 text-xs text-white/70 outline-none focus:border-[#ff4d3d]/60"><option value="all">全部状态</option><option value="draft">未上传</option><option value="pending">待审核</option><option value="ready">可发布</option><option value="published">已发布</option></select></label>
    </div>
    {!current && <div aria-label="正在加载投稿" className="divide-y divide-white/6 px-6">{[0, 1, 2].map(i => <div key={i} className="flex animate-pulse gap-5 py-6"><div className="h-24 w-36 shrink-0 rounded-lg bg-white/5" /><div className="flex-1 space-y-3 py-2"><div className="h-4 w-2/3 rounded bg-white/5" /><div className="h-3 w-1/2 rounded bg-white/5" /><div className="h-3 w-20 rounded bg-white/5" /></div></div>)}</div>}
    {current?.error && <div className="px-6 py-14 text-center"><p role="alert" className="text-sm text-red-300">{current.error}</p><button type="button" onClick={() => setRefresh(value => value + 1)} className="creator-secondary mt-5"><RefreshCw className="size-4" />重新加载</button></div>}
    {page && !filtered.length && <div className="flex min-h-80 flex-col items-center justify-center px-6 py-12 text-center"><div className="mb-5 grid size-16 place-items-center rounded-2xl border border-white/8 bg-white/4 text-white/35"><FileVideo className="size-7" /></div><h2 className="font-semibold">{page.items.length ? '没有找到匹配的稿件' : '你的下一份作品，从这里开始'}</h2><p className="mt-2 text-sm text-white/40">{page.items.length ? '试试其他关键词或切换本页状态。' : '上传一段视频，把值得记录的瞬间分享出去。'}</p>{page.items.length ? <button type="button" onClick={() => { setQuery(''); setStatus('all'); }} className="creator-secondary mt-6">清除筛选</button> : <Link href="/upload" className="creator-primary mt-6"><Plus className="size-4" />发布第一条视频</Link>}</div>}
    <div className="divide-y divide-white/6 px-5 sm:px-6">
      {filtered.map((video, index) => <article key={video.video_id} className="group flex flex-col gap-4 py-6 transition sm:flex-row sm:gap-5">
        <Link href={`/upload?video_id=${encodeURIComponent(video.video_id)}`} aria-label={`管理视频：${video.title}`} className={`relative grid aspect-video w-full shrink-0 place-items-center overflow-hidden rounded-lg border border-white/8 bg-gradient-to-br sm:w-40 xl:w-48 ${index % 2 ? 'from-[#17303a] to-[#10141b]' : 'from-[#33252e] to-[#12151d]'}`}>
          <div className="pointer-events-none absolute inset-0 opacity-25 [background-image:linear-gradient(rgb(255_255_255/.08)_1px,transparent_1px),linear-gradient(90deg,rgb(255_255_255/.08)_1px,transparent_1px)] [background-size:24px_24px]" />
          <Clapperboard className="size-9 text-white/25 transition group-hover:text-white/50" />
          <span className="absolute bottom-2 left-2 rounded bg-black/35 px-1.5 py-0.5 text-[10px] text-white/50">{video.has_media ? '视频稿件' : '待上传'}</span>
          {video.duration_ms > 0 && <span className="absolute right-2 bottom-2 rounded bg-black/60 px-1.5 py-0.5 font-mono text-[10px] text-white/80">{videoDuration(video.duration_ms)}</span>}
        </Link>
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="flex flex-wrap items-center gap-3"><VideoStatusBadge video={video} /><span className="text-[11px] text-white/30">{videoDate(video.created_at)} 创建</span></div>
          <Link href={`/upload?video_id=${encodeURIComponent(video.video_id)}`} className="mt-2.5 line-clamp-2 break-words text-[15px] font-semibold leading-6 transition hover:text-[#ff786c]">{video.title}</Link>
          <p className="mt-1.5 line-clamp-1 text-xs text-white/35">{video.description || '这条作品还没有简介'}</p>
          <p className="mt-3 text-[10px] tracking-wide text-white/25">稿件 ID · {video.video_id}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2 sm:flex-col sm:justify-center sm:items-stretch xl:min-w-28">
          <Link href={`/upload?video_id=${encodeURIComponent(video.video_id)}`} className={canPublishVideo(video) ? 'creator-primary' : 'creator-secondary'}>{canPublishVideo(video) ? '去发布' : !video.has_media && video.publish_status === 'draft' ? '继续投稿' : '管理稿件'}<ArrowRight className="size-3.5" /></Link>
          {isPublicVideo(video) && <Link href={`/video/${video.video_id}`} className="px-3 py-2 text-center text-xs text-white/40 transition hover:text-white">观看视频 ↗</Link>}
        </div>
      </article>)}
    </div>
    {page && <div className="flex flex-wrap items-center justify-between gap-3 border-t border-white/8 px-5 py-4 sm:px-6"><p className="text-xs text-white/35">本页显示 {filtered.length} / {page.items.length} 条<span className="mx-2 text-white/15">·</span>按创建时间倒序</p><div className="flex gap-2">{cursor && <button type="button" onClick={() => goTo('')} className="creator-secondary py-2 text-xs"><ChevronLeft className="size-3.5" />第一页</button>}<button type="button" disabled={!page.has_more} onClick={() => goTo(page.next_cursor)} className="creator-secondary py-2 text-xs">{page.has_more ? '下一页' : '已到最后一页'}<ChevronRight className="size-3.5" /></button></div></div>}
  </section>;
}

export default function StudioPage() {
  const { user, loading, apiBase } = useAuth();
  return <CreatorShell active="studio">
    <div className="flex flex-wrap items-center justify-between gap-4">
      <div><p className="mb-2 text-[10px] font-semibold tracking-[.2em] text-[#ff786c]">YOUR CREATIVE SPACE</p><h1 className="text-2xl font-bold tracking-tight sm:text-3xl">稿件管理</h1><p className="mt-2 text-sm text-white/40">从一个灵感，到一次被看见。</p></div>
      <Link href="/upload" className="creator-primary"><Plus className="size-4" />发布视频</Link>
    </div>
    {loading || !user ? <CreatorAccess loading={loading} /> : <MyVideos key={`${apiBase}:${user.user_id}`} apiBase={apiBase} />}
  </CreatorShell>;
}
