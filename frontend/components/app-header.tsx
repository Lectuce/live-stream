'use client';

import Link from 'next/link';
import { useState } from 'react';
import { Compass, LogOut, Radio, UserRound } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';

export function AppHeader() {
  const { user, loading, logout } = useAuth();
  const [authOpen, setAuthOpen] = useState(false);

  return (
    <>
      <header className="glass sticky top-0 z-40 border-b border-white/8">
        <div className="mx-auto flex h-16 max-w-[1480px] items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link href="/" className="flex items-center gap-3" aria-label="返回直播首页">
            <span className="grid size-9 place-items-center rounded-xl bg-[#ff4d3d] shadow-[0_0_25px_rgb(255_77_61/28%)]"><Radio className="size-5" /></span>
            <span><span className="block text-sm font-black tracking-[.16em]">LIVE NOW</span><span className="hidden text-[10px] tracking-[.18em] text-white/35 sm:block">REALTIME STAGE</span></span>
          </Link>
          <nav className="flex items-center gap-2">
            <Link href="/feed" className="inline-flex h-9 items-center gap-1.5 rounded-md px-2 text-xs font-semibold text-white/65 transition hover:bg-white/5 hover:text-white sm:px-3"><Compass className="size-4" /><span className="hidden sm:inline">发现房间</span></Link>
            {!loading && user ? <div className="flex items-center gap-2"><div className="hidden items-center gap-2 rounded-full border border-white/8 bg-white/4 py-1 pl-1 pr-3 sm:flex"><Avatar size="sm"><AvatarImage src={user.avatar_url || undefined} /><AvatarFallback>{user.user_name.slice(0, 1).toUpperCase()}</AvatarFallback></Avatar><span className="text-xs text-white/70">{user.user_name}</span></div><Button variant="ghost" size="icon" onClick={() => void logout()} aria-label="退出登录"><LogOut /></Button></div> : <Button size="sm" onClick={() => setAuthOpen(true)} className="bg-white text-black hover:bg-white/85"><UserRound /> 登录</Button>}
          </nav>
        </div>
      </header>
      <AuthDialog open={authOpen} onOpenChange={setAuthOpen} />
    </>
  );
}
