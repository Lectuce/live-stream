'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { ArrowRight, Flame, Radio, RefreshCw, Search, Sparkles, Users } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { RoomCard } from '@/components/room-card';
import { RoomStudio } from '@/components/room-studio';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { api } from '@/lib/api';
import type { Room } from '@/lib/types';
import { useAuth } from '@/components/auth-provider';
import type { LucideIcon } from 'lucide-react';

export default function Home() {
  const { apiBase } = useAuth();
  const [rooms, setRooms] = useState<Room[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const load = useCallback(async () => {
    setLoading(true); setError('');
    try { setRooms((await api.listRooms(apiBase)).data || []); }
    catch (err) { setError(err instanceof Error ? err.message : '读取房间失败'); }
    finally { setLoading(false); }
  }, [apiBase]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);
  const filtered = useMemo(() => rooms.filter((room) => `${room.room_title} ${room.owner_username || ''}`.toLowerCase().includes(query.toLowerCase())), [query, rooms]);
  const totalLikes = rooms.reduce((sum, room) => sum + (room.like_count ?? room.praise_count ?? 0), 0);
  const metrics: Array<{ label: string; value: string | number; Icon: LucideIcon }> = [
    { label: '在线直播', value: rooms.length, Icon: Radio }, { label: '累计点赞', value: totalLikes, Icon: Flame },
    { label: '互动方式', value: '实时弹幕', Icon: Sparkles }, { label: '历史消息', value: '随时回看', Icon: RefreshCw },
  ];

  return (
    <div className="min-h-screen">
      <AppHeader />
      <main className="mx-auto max-w-[1480px] px-4 pb-20 pt-8 sm:px-6 lg:px-8">
        <section className="relative overflow-hidden rounded-2xl border border-white/9 bg-[#10131a] px-6 py-10 sm:px-10 lg:grid lg:grid-cols-[1.25fr_.75fr] lg:items-end lg:px-14 lg:py-14">
          <div className="relative z-10 max-w-3xl">
            <div className="mb-5 flex items-center gap-2 text-xs font-bold tracking-[.18em] text-[#ff6b5e]"><span className="live-dot size-2 rounded-full bg-[#ff4d3d]" /> 精彩直播 · 正在发生</div>
            <h1 className="max-w-2xl text-4xl font-black leading-[1.05] tracking-[-.04em] sm:text-6xl">把这一刻，<br /><span className="text-white/38">同步给所有人。</span></h1>
            <p className="mt-6 max-w-xl text-sm leading-7 text-white/50 sm:text-base">发现正在发生的精彩直播，用弹幕分享感受，为喜欢的主播送上点赞。</p>
            <div className="mt-8 flex flex-wrap gap-3"><Link href="/feed"><Button className="h-11 bg-[#ff4d3d] px-5 hover:bg-[#ff6356]">滑动发现房间 <ArrowRight /></Button></Link><a href="#live-rooms"><Button variant="outline" className="h-11 border-white/12 bg-white/4 px-5">查看房间列表</Button></a><a href="#studio"><Button variant="outline" className="h-11 border-white/12 bg-white/4 px-5">我的直播间</Button></a></div>
          </div>
          <div className="relative mt-10 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-white/8 bg-white/8 lg:mt-0">
            {metrics.map(({ label, value, Icon }) => <div key={label} className="bg-[#0d1016] p-5"><Icon className="mb-4 size-4 text-[#ff6255]" /><div className="text-xl font-bold">{value}</div><div className="mt-1 text-[11px] tracking-wide text-white/35">{label}</div></div>)}
          </div>
          <div aria-hidden className="absolute -right-20 -top-24 size-80 rounded-full border border-[#ff4d3d]/15 shadow-[0_0_100px_rgb(255_77_61/8%)]" />
        </section>
        <section id="studio" className="mt-8"><RoomStudio onChanged={load} /></section>
        <section id="live-rooms" className="mt-14 scroll-mt-24">
          <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
            <div><div className="mb-2 flex items-center gap-2 text-xs font-semibold tracking-[.14em] text-[#ff6558]"><Flame className="size-4" /> ON AIR</div><h2 className="text-2xl font-bold tracking-tight sm:text-3xl">正在直播</h2><p className="mt-2 text-sm text-white/40">按热度排列的公开直播间</p></div>
            <div className="relative w-full sm:w-72"><Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-white/30" /><Input value={query} onChange={(e) => setQuery(e.target.value)} className="h-10 border-white/9 bg-white/4 pl-9" placeholder="搜索房间或主播" /></div>
          </div>
          {loading ? <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="aspect-[1.12] rounded-xl bg-white/5" />)}</div> : error ? <div className="rounded-xl border border-red-400/15 bg-red-400/5 p-8 text-center"><p className="text-red-300">{error}</p><Button variant="outline" className="mt-4" onClick={() => void load()}><RefreshCw />重试</Button></div> : filtered.length ? <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{filtered.map((room, index) => <RoomCard key={room.room_id} room={room} index={index} />)}</div> : <div className="grid min-h-56 place-items-center rounded-xl border border-dashed border-white/10 bg-white/[.02] text-center"><div><Users className="mx-auto mb-3 size-7 text-white/20" /><p className="font-medium">{query ? '没有匹配的直播间' : '暂时还没有人开播'}</p><p className="mt-1 text-sm text-white/35">创建你的房间，成为第一个开播的人。</p></div></div>}
        </section>
      </main>
      <footer className="border-t border-white/8 px-4 py-8 text-center text-xs text-white/28">LIVE NOW · 分享此刻，共同在场</footer>
    </div>
  );
}
