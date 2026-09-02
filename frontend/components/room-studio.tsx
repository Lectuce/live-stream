'use client';

import Link from 'next/link';
import { useCallback, useEffect, useState } from 'react';
import { ArrowRight, CircleStop, LoaderCircle, Pencil, Play, Plus, Radio } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { useAuth } from '@/components/auth-provider';
import { api, ApiError } from '@/lib/api';
import type { Room } from '@/lib/types';

export function RoomStudio({ onChanged }: { onChanged: () => void | Promise<void> }) {
  const { user, apiBase, loading: authLoading } = useAuth();
  const [room, setRoom] = useState<Room | null>(null);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [error, setError] = useState('');
  const [action, setAction] = useState<'create' | 'rename' | null>(null);
  const loadMine = useCallback(async () => {
    if (!user) { setRoom(null); return; }
    setLoading(true);
    try { setRoom((await api.myRoom(apiBase)).data); }
    catch (err) { if (!(err instanceof ApiError && err.status === 401)) setError(err instanceof Error ? err.message : '查询失败'); }
    finally { setLoading(false); }
  }, [apiBase, user]);
  useEffect(() => { queueMicrotask(() => void loadMine()); }, [loadMine]);

  async function save() {
    if (!title.trim()) return;
    setLoading(true); setError('');
    try {
      const result = action === 'rename' && room ? await api.updateRoom(apiBase, room.room_id, title.trim()) : await api.createRoom(apiBase, title.trim());
      setRoom(result.data); setOpen(false); await onChanged();
    } catch (err) { setError(err instanceof Error ? err.message : '保存失败'); }
    finally { setLoading(false); }
  }

  async function toggleLive() {
    if (!room) return;
    setLoading(true); setError('');
    try {
      if (room.status === 1) await api.stopRoom(apiBase, room.room_id);
      else await api.startRoom(apiBase, room.room_id, room.room_title);
      await loadMine(); await onChanged();
    } catch (err) { setError(err instanceof Error ? err.message : '操作失败'); }
    finally { setLoading(false); }
  }

  if (authLoading) return <div className="h-28 animate-pulse rounded-xl bg-white/4" />;
  if (!user) return <div className="flex flex-col justify-between gap-5 rounded-xl border border-white/8 bg-[#0e1117] p-5 sm:flex-row sm:items-center"><div><p className="text-xs font-semibold tracking-[.13em] text-white/35">CREATOR STUDIO</p><h2 className="mt-2 text-xl font-bold">登录后创建你的直播间</h2><p className="mt-1 text-sm text-white/40">直播间归属由 Session 决定，不会相信客户端传入的 owner_id。</p></div><div className="flex size-12 shrink-0 items-center justify-center rounded-xl border border-white/8 bg-white/4"><Radio className="text-[#ff6255]" /></div></div>;

  return (
    <>
      <div className="rounded-xl border border-white/8 bg-[#0e1117] p-5 sm:p-6">
        <div className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-4"><div className="grid size-12 shrink-0 place-items-center rounded-xl bg-[#ff4d3d]/10 text-[#ff6255]"><Radio /></div><div><p className="text-[10px] font-semibold tracking-[.16em] text-white/35">CREATOR STUDIO</p><h2 className="mt-1 text-xl font-bold">{room ? room.room_title : '还没有直播间'}</h2><p className="mt-1 text-sm text-white/38">{room ? (room.status === 1 ? '直播进行中 · 所有观众均可加入' : `房间 #${room.room_id} · 当前未开播`) : '每个账号可创建一个直播间'}</p></div></div>
          <div className="flex flex-wrap gap-2">{room ? <><Button variant="outline" onClick={() => { setAction('rename'); setTitle(room.room_title); setOpen(true); }}><Pencil />改名</Button><Button onClick={() => void toggleLive()} disabled={loading} className={room.status === 1 ? 'bg-white text-black hover:bg-white/80' : 'bg-[#ff4d3d] hover:bg-[#ff6356]'}>{loading ? <LoaderCircle className="animate-spin" /> : room.status === 1 ? <CircleStop /> : <Play />}{room.status === 1 ? '结束直播' : '开始直播'}</Button>{room.status === 1 && <Link href={`/rooms/${room.room_id}`}><Button variant="ghost">进入房间 <ArrowRight /></Button></Link>}</> : <Button onClick={() => { setAction('create'); setTitle(''); setOpen(true); }} className="bg-[#ff4d3d] hover:bg-[#ff6356]"><Plus />创建房间</Button>}</div>
        </div>
        {error && <p className="mt-4 rounded-lg bg-red-400/8 px-3 py-2 text-sm text-red-300">{error}</p>}
      </div>
      <Dialog open={open} onOpenChange={setOpen}><DialogContent className="border-white/10 bg-[#11151d]"><DialogHeader><DialogTitle>{action === 'rename' ? '修改房间标题' : '创建直播间'}</DialogTitle><DialogDescription>标题长度为 1–100 个字符，之后仍可修改。</DialogDescription></DialogHeader><div className="space-y-3"><Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={100} className="h-11" placeholder="例如：今晚一起写 Go" /><Button onClick={() => void save()} disabled={loading || !title.trim()} className="h-10 w-full">{loading && <LoaderCircle className="animate-spin" />}保存</Button></div></DialogContent></Dialog>
    </>
  );
}
