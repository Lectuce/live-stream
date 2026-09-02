import Link from 'next/link';
import { ArrowUpRight, Heart, Radio } from 'lucide-react';
import type { Room } from '@/lib/types';

const palettes = ['from-[#30223d] to-[#11141d]', 'from-[#17313d] to-[#10141a]', 'from-[#3a261f] to-[#111319]', 'from-[#1f2f27] to-[#101419]'];

export function RoomCard({ room, index }: { room: Room; index: number }) {
  const likes = room.like_count ?? room.praise_count ?? 0;
  return (
    <Link href={`/rooms/${room.room_id}`} className="group overflow-hidden rounded-xl border border-white/8 bg-[#0e1117] transition duration-300 hover:-translate-y-1 hover:border-white/18 hover:shadow-[0_22px_55px_rgb(0_0_0/30%)]">
      <div className={`relative aspect-video overflow-hidden bg-gradient-to-br ${palettes[index % palettes.length]}`}>
        <div className="absolute inset-0 opacity-30 [background-image:linear-gradient(rgb(255_255_255/.05)_1px,transparent_1px),linear-gradient(90deg,rgb(255_255_255/.05)_1px,transparent_1px)] [background-size:28px_28px]" />
        <div className="absolute inset-0 grid place-items-center"><div className="grid size-16 place-items-center rounded-full border border-white/10 bg-black/20 text-white/45 transition group-hover:scale-110 group-hover:text-white"><Radio className="size-7" /></div></div>
        <div className="absolute left-3 top-3 flex items-center gap-1.5 rounded-md bg-[#ff4d3d] px-2 py-1 text-[10px] font-black tracking-wider"><span className="live-dot size-1.5 rounded-full bg-white" /> LIVE</div>
        <div className="absolute bottom-3 right-3 flex items-center gap-1 rounded-md bg-black/45 px-2 py-1 text-[11px] backdrop-blur"><Heart className="size-3" />{likes.toLocaleString()}</div>
      </div>
      <div className="flex items-start justify-between gap-3 p-4"><div className="min-w-0"><h3 className="truncate font-semibold tracking-tight">{room.room_title}</h3><p className="mt-1 truncate text-xs text-white/38">{room.owner_username || `主播 #${room.owner_id}`}</p></div><ArrowUpRight className="mt-0.5 size-4 shrink-0 text-white/25 transition group-hover:text-[#ff6255]" /></div>
    </Link>
  );
}
