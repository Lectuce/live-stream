'use client';

import { useEffect } from 'react';
import Link from 'next/link';
import { ArrowUpRight, Heart, MessageCircle, Radio, Signal, Users } from 'lucide-react';
import { api } from '@/lib/api';
import type { Room } from '@/lib/types';
import { useLiveRoom } from '@/hooks/use-live-room';

const backgrounds = [
  'from-[#35202b] via-[#1c1725] to-[#0c111b]',
  'from-[#182f37] via-[#17212b] to-[#0c111b]',
  'from-[#35281d] via-[#211c20] to-[#0c111b]',
  'from-[#1c3028] via-[#192326] to-[#0c111b]',
];

type Props = {
  room: Room;
  index: number;
  total: number;
  active: boolean;
  apiBase: string;
  onRoomEnded: (roomId: number) => void;
};


  export function LiveFeedItem({
    room,
    index,
    total,
    active,
    apiBase,
    onRoomEnded,
  }: Props) {
  const {
    messages,
    appendMessages,
    connection,
    onlineCount,
    likeCount,
    roomStatus,
    error,
  } = useLiveRoom(
    apiBase,
    room.room_id,
    active,
    room.like_count ?? room.praise_count ?? 0,
  );

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    void api.history(apiBase, room.room_id, 0, 20).then((result) => {
      if (!cancelled) appendMessages(result.data.items ?? []);
    }).catch(() => {
      // 即使历史记录不可用，实时 WebSocket 仍然有用。
    });
    return () => { cancelled = true; };
  }, [active, apiBase, room.room_id, appendMessages]);

  useEffect(() => {
    if (active && roomStatus === 0) {
      onRoomEnded(room.room_id);
    }
  }, [
    active,
    onRoomEnded,
    room.room_id,
    roomStatus,
  ]);



  const recentMessages = messages.slice(-6);
  const statusText = connection === 'open' ? '互动进行中' : connection === 'connecting' ? '正在加入' : connection === 'error' ? '互动暂时中断' : '等待进入';

  return (
    <section className="relative h-full snap-start snap-always overflow-hidden px-3 py-3 sm:px-6 sm:py-5" aria-label={`${room.room_title}，第 ${index + 1} 个房间`}>
      <div className={`pointer-events-none absolute inset-0 bg-gradient-to-br ${backgrounds[index % backgrounds.length]} opacity-60`} />
      <div className="relative mx-auto flex h-full max-w-[1350px] min-h-0 flex-col gap-3 sm:gap-4">
        <div className="flex shrink-0 items-center justify-between gap-3 px-1">
          <span className="flex items-center gap-2 text-[11px] font-semibold tracking-[.18em] text-white/55"><Radio className="size-4 text-[#ff6255]" /> 房间发现</span>
          <span className="font-mono text-xs text-white/45">{String(index + 1).padStart(2, '0')} / {String(total).padStart(2, '0')}</span>
        </div>
        <div className="grid min-h-0 flex-1 grid-rows-[minmax(0,1fr)_minmax(160px,0.52fr)] gap-3 lg:grid-cols-[minmax(0,1.45fr)_minmax(300px,0.55fr)] lg:grid-rows-1 lg:gap-4">
          <div className="relative flex min-h-0 flex-col justify-between overflow-hidden rounded-2xl border border-white/10 bg-[#0b0e15]/60 p-5 shadow-[0_25px_70px_rgb(0_0_0/25%)] sm:p-8">
            <div className={`pointer-events-none absolute -right-20 -top-24 size-80 rounded-full bg-gradient-to-br ${backgrounds[index % backgrounds.length]} opacity-90 blur-3xl`} />
            <div className="relative flex items-start justify-between gap-3">
              <span className="inline-flex items-center gap-2 rounded-full border border-[#ff6255]/25 bg-[#ff6255]/10 px-3 py-1.5 text-[11px] font-semibold text-[#ff9a91]"><span className="live-dot size-1.5 rounded-full bg-[#ff6255]" /> 互动房间</span>
              <span className="rounded-full border border-white/10 bg-black/20 px-3 py-1.5 font-mono text-[11px] text-white/55">#{room.room_id}</span>
            </div>
            <div className="relative my-4 flex min-h-0 flex-1 flex-col items-center justify-center text-center">
              <div className="mb-5 grid size-20 place-items-center rounded-[1.7rem] border border-white/10 bg-white/[.04] text-[#ff8b7e] shadow-[0_0_70px_rgb(255_77_61/12%)] sm:size-24"><Radio className="size-9 sm:size-11" /></div>
              <p className="text-xl font-bold tracking-tight sm:text-3xl">这里正在发生实时互动</p>
              <p className="mt-2 max-w-md text-xs leading-6 text-white/45 sm:text-sm">当前尚未接入直播视频信号。进入房间即可查看完整弹幕并参与互动。</p>
            </div>
            <div className="relative flex shrink-0 flex-col gap-4 border-t border-white/10 pt-4 sm:flex-row sm:items-end sm:justify-between">
              <div className="min-w-0">
                <p className="text-xs text-white/45">{room.owner_username || `主播 #${room.owner_id}`}</p>
                <h1 className="mt-1 line-clamp-2 text-xl font-bold tracking-tight sm:text-3xl">{room.room_title}</h1>
                <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-white/55">
                  <span className="inline-flex items-center gap-1.5"><Users className="size-3.5" /> {connection === 'open' ? onlineCount.toLocaleString() : '—'} 在线</span>
                  <span className="inline-flex items-center gap-1.5"><Heart className="size-3.5" /> {likeCount.toLocaleString()} 点赞</span>
                  {typeof room.hot_score === 'number' && <span>热度 {Math.round(room.hot_score).toLocaleString()}</span>}
                </div>
              </div>
              <Link href={`/rooms/${room.room_id}`} className="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-xl bg-[#ff4d3d] px-5 text-sm font-semibold text-white transition hover:bg-[#ff6356] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white">进入房间 <ArrowUpRight className="size-4" /></Link>
            </div>
          </div>
          <div className="flex min-h-0 flex-col overflow-hidden rounded-2xl border border-white/10 bg-[#0b0e15]/75">
            <div className="flex shrink-0 items-center justify-between border-b border-white/10 px-4 py-3 sm:px-5 sm:py-4">
              <div className="flex items-center gap-2 text-sm font-semibold"><MessageCircle className="size-4 text-[#ff8b7e]" /> 实时弹幕</div>
              <span className={`flex items-center gap-1.5 text-[11px] ${connection === 'open' ? 'text-emerald-300' : 'text-white/45'}`}><Signal className="size-3" /> {statusText}</span>
            </div>
            <div className="flex min-h-0 flex-1 flex-col justify-end gap-2 overflow-hidden px-4 py-3 sm:px-5">
              {recentMessages.length ? recentMessages.map((message) => (
                <div key={message.message_id || `${message.room_seq}-${message.id}`} className="rounded-lg bg-white/[.035] px-3 py-2 text-xs leading-5">
                  <span className="mr-2 font-semibold text-[#ff9a91]">{message.user_name || `用户 ${message.user_id}`}</span>
                  <span className="break-words text-white/75">{message.content}</span>
                </div>
              )) : <p className="py-4 text-center text-xs text-white/40">还没有弹幕，进入房间发送第一条消息</p>}
            </div>
            {error && active && <p className="shrink-0 border-t border-white/10 px-4 py-2 text-[11px] text-amber-300 sm:px-5">{error}</p>}
            <div className="shrink-0 border-t border-white/10 px-4 py-3 text-[11px] text-white/35 sm:px-5">滑动切换房间，发现更多精彩直播</div>
          </div>
        </div>
      </div>
    </section>
  );
}
