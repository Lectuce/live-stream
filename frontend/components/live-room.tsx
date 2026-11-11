'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, CircleStop, Clock3, Heart, History, LoaderCircle, MessageCircle, Radio, Send, Signal, SignalLow, Users } from 'lucide-react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { AuthDialog } from '@/components/auth-dialog';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { useAuth } from '@/components/auth-provider';
import { useLiveRoom } from '@/hooks/use-live-room';
import { api } from '@/lib/api';
import type { DanmakuMessage, Room } from '@/lib/types';

const stateLabels = { connecting: '正在加入', open: '互动中', recovering: '正在同步消息', closed: '已离开', error: '互动暂时中断' } as const;

export function LiveRoom({ roomId }: { roomId: number }) {
  const { apiBase, user } = useAuth();
  const [room, setRoom] = useState<Room | null>(null);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState('');
  const [content, setContent] = useState('');
  const [authOpen, setAuthOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [hasMoreHistory, setHasMoreHistory] = useState(true);
  const [stopping, setStopping] = useState(false);
  const chatEndRef = useRef<HTMLDivElement>(null);
  const oldestHistoryIDRef = useRef(0);
  const { messages, appendMessages, connection, onlineCount, likeCount, deliveryMode, roomStatus, error, send, likeSendState } = useLiveRoom(apiBase, roomId, room?.status === 1, room?.like_count || 0);

  const loadRoom = useCallback(async () => {
    setLoading(true); setPageError('');
    try { const result = await api.roomDetail(apiBase, roomId); setRoom({ ...result.data, room_id: result.data.room_id || roomId }); }
    catch (err) { setPageError(err instanceof Error ? err.message : '房间读取失败'); }
    finally { setLoading(false); }
  }, [apiBase, roomId]);

  const loadHistory = useCallback(async (older = false) => {
    setHistoryLoading(true);
    try {
      const oldest = older ? oldestHistoryIDRef.current : 0;
      const result = (await api.history(apiBase, roomId, oldest, 50)).data;
      const pageOldest = result.items.reduce((min, item) => item.id ? Math.min(min, item.id) : min, Number.MAX_SAFE_INTEGER);
      if (pageOldest !== Number.MAX_SAFE_INTEGER) oldestHistoryIDRef.current = pageOldest;
      setHasMoreHistory(result.has_more);
      appendMessages(result.items);
    } catch (err) { setPageError(err instanceof Error ? err.message : '历史消息读取失败'); }
    finally { setHistoryLoading(false); }
  }, [apiBase, appendMessages, roomId]);

  useEffect(() => { queueMicrotask(() => void loadRoom()); }, [loadRoom]);
  useEffect(() => { if (room?.status === 1) queueMicrotask(() => void loadHistory(false)); }, [loadHistory, room?.status]);
  useEffect(() => { chatEndRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [messages.length]);
  useEffect(() => {
    if (roomStatus === null) return;

    setRoom((current) =>
      current ? { ...current, status: roomStatus } : current,
    );
  }, [roomStatus]);



  function submit(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!user) { setAuthOpen(true); return; }
    const text = content.trim();
    if (!text) return;
    if (send('danma', text)) setContent('');
  }

  function like() {
    if (!user) {
      setAuthOpen(true);
      return;
    }
    send('like');
  }

  async function stopRoom() {
    setStopping(true);
    try { await api.stopRoom(apiBase, roomId); await loadRoom(); }
    catch (err) { setPageError(err instanceof Error ? err.message : '关播失败'); }
    finally { setStopping(false); }
  }

  if (loading) return <><AppHeader /><main className="mx-auto max-w-[1480px] p-4 sm:p-6 lg:p-8"><Skeleton className="aspect-video w-full rounded-xl bg-white/5" /></main></>;
  if (pageError && !room) return <><AppHeader /><main className="grid min-h-[70vh] place-items-center p-6 text-center"><div><Radio className="mx-auto mb-4 size-10 text-white/20" /><h1 className="text-xl font-bold">无法打开直播间</h1><p className="mt-2 text-sm text-red-300">{pageError}</p><Link href="/"><Button variant="outline" className="mt-5"><ArrowLeft />返回首页</Button></Link></div></main></>;
  if (!room) return null;

  const isOwner = user?.user_id === room.owner_id;
  const overlayMessages = messages.slice(-18);
  return (
    <div className="min-h-screen bg-[#080a0f]">
      <AppHeader />
      <main className="mx-auto max-w-[1540px] px-3 pb-10 pt-4 sm:px-6 lg:px-8">
        <div className="mb-4 flex items-center justify-between gap-3"><Link href="/" className="flex items-center gap-2 text-sm text-white/45 transition hover:text-white"><ArrowLeft className="size-4" />全部直播</Link><div className="flex items-center gap-2"><Badge variant="outline" className="border-white/10 bg-white/4 text-white/55">房间 #{roomId}</Badge>{isOwner && room.status === 1 && <Button variant="destructive" size="sm" onClick={() => void stopRoom()} disabled={stopping}>{stopping ? <LoaderCircle className="animate-spin" /> : <CircleStop />}结束直播</Button>}</div></div>
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
          <section className="min-w-0">
            <div className="relative aspect-video overflow-hidden rounded-xl border border-white/9 bg-black shadow-[0_30px_80px_rgb(0_0_0/35%)]">
              {room.status === 1 ? <>{room.play_url && !room.play_url.includes('live.example.com') ? <video className="size-full object-contain" src={room.play_url} controls autoPlay playsInline><track kind="captions" srcLang="zh" label="暂无字幕" src="data:text/vtt,WEBVTT" /></video> : <div className="absolute inset-0 grid place-items-center bg-[#090b10]"><div className="text-center"><div className="mx-auto grid size-20 place-items-center rounded-full border border-white/8 bg-white/[.025]"><Radio className="size-8 text-[#ff6255]" /></div><p className="mt-5 font-semibold">主播暂未开启画面</p><p className="mt-2 text-xs text-white/30">可以先在弹幕区和大家聊聊</p></div></div>}<div className="pointer-events-none absolute inset-0 overflow-hidden" aria-label="弹幕层">{overlayMessages.map((message, index) => <div key={message.message_id} className="danmaku-fly absolute right-0 max-w-[70%] whitespace-nowrap rounded-full bg-black/45 px-3 py-1.5 text-sm font-medium text-white shadow backdrop-blur-sm" style={{ top: `${8 + (index % 7) * 11}%`, animationDuration: `${8 + (index % 5)}s` }}>{message.content}</div>)}</div></> : <div className="absolute inset-0 grid place-items-center"><div className="text-center"><CircleStop className="mx-auto size-10 text-white/20" /><h2 className="mt-4 text-xl font-bold">直播已经结束</h2><p className="mt-2 text-sm text-white/35">历史弹幕仍可在右侧查看</p></div></div>}
              <div className="absolute left-3 top-3 flex items-center gap-2"><span className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-[10px] font-bold ${room.status === 1 ? 'bg-[#ff4d3d]' : 'bg-white/10'}`}><span className={`size-1.5 rounded-full bg-white ${room.status === 1 ? 'live-dot' : ''}`} />{room.status === 1 ? '直播中' : '已结束'}</span>{room.status === 1 && <span className="rounded-md bg-black/50 px-2 py-1 text-[10px] text-white/75 backdrop-blur">弹幕互动中</span>}</div>
            </div>
            <div className="mt-4 flex flex-col gap-4 rounded-xl border border-white/8 bg-[#0e1117] p-5 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-center gap-3"><Avatar size="lg"><AvatarFallback className="bg-[#ff4d3d]/10 text-[#ff6b5e]">{(room.owner_username || 'L').slice(0, 1)}</AvatarFallback></Avatar><div className="min-w-0"><h1 className="truncate text-xl font-bold tracking-tight sm:text-2xl">{room.room_title}</h1><p className="mt-1 text-sm text-white/38">{room.owner_username || `主播 #${room.owner_id}`}{room.start_at && ` · ${new Date(room.start_at).toLocaleString('zh-CN')}`}</p></div></div>
              <div className="flex items-center gap-2"><div className="flex items-center gap-4 pr-2 text-sm text-white/45"><span className="flex items-center gap-1.5"><Users className="size-4" />{onlineCount}</span><span className="flex items-center gap-1.5"><Heart className={likeSendState === 'accepted' ? 'size-4 fill-[#ff6255] text-[#ff6255]' : 'size-4'} />{likeCount}</span></div><Button type="button" variant="outline" disabled={room.status !== 1 || connection !== 'open'} onClick={like} className="min-w-24 border-white/10 bg-white/4 hover:border-[#ff4d3d]/30 hover:text-[#ff6b5e]"><Heart className={likeSendState === 'accepted' ? 'fill-[#ff6255] text-[#ff6255]' : ''} />{likeSendState === 'accepted' ? '已点赞' : likeSendState === 'error' ? '重试' : '点赞'}</Button></div>
            </div>
            {(pageError || error) && <div className="mt-4 rounded-lg border border-red-400/15 bg-red-400/5 px-4 py-3 text-sm text-red-300">{pageError || error}</div>}
          </section>

          <aside className="flex min-h-[560px] flex-col overflow-hidden rounded-xl border border-white/8 bg-[#0e1117] xl:h-[calc(100vh-112px)] xl:max-h-[850px]">
            <div className="flex h-14 shrink-0 items-center justify-between border-b border-white/8 px-4"><div><h2 className="text-sm font-semibold">实时弹幕</h2><div className="mt-0.5 flex items-center gap-1 text-[10px] text-white/35">{connection === 'open' ? <Signal className="size-3 text-emerald-400" /> : <SignalLow className="size-3 text-amber-400" />}{stateLabels[connection]} · {messages.length} 条</div></div><Button variant="ghost" size="icon-sm" onClick={() => setHistoryOpen(true)} aria-label="查看历史弹幕"><History /></Button></div>
            <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
              {messages.length ? <div className="space-y-1">{messages.map((message) => <ChatMessage key={message.message_id} message={message} />)}<div ref={chatEndRef} /></div> : <div className="grid h-full min-h-72 place-items-center text-center"><div><MessageCircle className="mx-auto size-7 text-white/15" /><p className="mt-3 text-sm text-white/38">还没有弹幕</p><p className="mt-1 text-xs text-white/22">说点什么，打破安静</p></div></div>}
            </div>
            <form onSubmit={submit} className="shrink-0 border-t border-white/8 p-3"><div className="flex gap-2"><Input value={content} onChange={(e) => setContent(e.target.value)} maxLength={500} disabled={room.status !== 1} placeholder={!user ? '登录后发送弹幕' : room.status === 1 ? '发送一条友善的弹幕…' : '直播已结束'} className="h-10 bg-white/4" /><Button type="submit" size="icon-lg" disabled={room.status !== 1 || connection !== 'open'} className="bg-[#ff4d3d] hover:bg-[#ff6356]" aria-label="发送弹幕"><Send /></Button></div><div className="mt-2 flex items-center justify-between px-1 text-[10px] text-white/25"><span>{deliveryMode === 'sampled' ? '直播间人气火热，消息持续更新中' : '友善互动，一起分享此刻'}</span><span>{content.length}/500</span></div></form>
          </aside>
        </div>
      </main>
      <AuthDialog open={authOpen} onOpenChange={setAuthOpen} />
      <Sheet open={historyOpen} onOpenChange={setHistoryOpen}><SheetContent className="border-white/10 bg-[#0d1016] sm:max-w-md"><SheetHeader className="border-b border-white/8"><SheetTitle className="flex items-center gap-2"><History className="size-4" />历史弹幕</SheetTitle><SheetDescription>回看直播间里大家分享过的内容。</SheetDescription></SheetHeader><div className="min-h-0 flex-1 overflow-y-auto px-4"><div className="py-3">{hasMoreHistory && <Button variant="ghost" size="sm" disabled={historyLoading} onClick={() => void loadHistory(true)} className="mb-3 w-full">{historyLoading ? <LoaderCircle className="animate-spin" /> : <Clock3 />}加载更早消息</Button>}{messages.map((message) => <ChatMessage key={`history-${message.message_id}`} message={message} />)}</div></div></SheetContent></Sheet>
    </div>
  );
}

function ChatMessage({ message }: { message: DanmakuMessage }) {
  return <div className="group flex gap-2 rounded-lg px-2 py-2 hover:bg-white/[.035]"><Avatar size="sm"><AvatarFallback className="bg-white/6 text-[10px] text-white/45">{(message.user_name || '?').slice(0, 1).toUpperCase()}</AvatarFallback></Avatar><div className="min-w-0 flex-1"><div className="flex items-center gap-2"><span className="truncate text-xs font-medium text-[#ff8a80]">{message.user_name || `用户 ${message.user_id}`}</span></div><p className="mt-0.5 break-words text-[13px] leading-5 text-white/72">{message.content}</p></div></div>;
}
