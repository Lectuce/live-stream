'use client';

import Link from 'next/link';
import { ArrowUpRight, Clapperboard, FileVideo, LayoutDashboard, Lightbulb, LoaderCircle, LogIn, Upload } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { AppHeader } from '@/components/app-header';
import { AuthDialog } from '@/components/auth-dialog';
import { useAuth } from '@/components/auth-provider';

export function CreatorShell({ active, children }: { active: 'studio' | 'upload'; children: ReactNode }) {
  const { user } = useAuth();
  return <>
    <AppHeader />
    <div className="mx-auto grid min-h-[calc(100svh-64px)] max-w-[1480px] lg:grid-cols-[216px_minmax(0,1fr)]">
      <aside className="border-b border-white/8 bg-[#0c0f15]/60 px-4 py-4 lg:border-r lg:border-b-0 lg:px-5 lg:py-8">
        <div className="lg:sticky lg:top-24">
          <div className="mb-8 hidden items-center gap-3 px-2 lg:flex">
            <div className="grid size-10 shrink-0 place-items-center rounded-xl border border-[#ff4d3d]/20 bg-[#ff4d3d]/10 text-[#ff786c]"><Clapperboard className="size-5" /></div>
            <div className="min-w-0"><p className="truncate text-sm font-semibold">{user?.user_name || '创作者工作台'}</p><p className="mt-1 text-[10px] tracking-[.14em] text-white/35">CREATOR STUDIO</p></div>
          </div>
          <nav aria-label="创作导航" className="flex gap-2 lg:flex-col">
            <Link href="/upload" aria-current={active === 'upload' ? 'page' : undefined} className={`creator-nav ${active === 'upload' ? 'creator-nav-active' : ''}`}><Upload className="size-4" />发布视频</Link>
            <Link href="/studio" aria-current={active === 'studio' ? 'page' : undefined} className={`creator-nav ${active === 'studio' ? 'creator-nav-active' : ''}`}><LayoutDashboard className="size-4" />稿件管理</Link>
            <Link href="/video" className="creator-nav hidden lg:flex"><FileVideo className="size-4" />视频广场<ArrowUpRight className="ml-auto size-3.5 text-white/30" /></Link>
          </nav>
          <div className="mt-12 hidden rounded-xl border border-white/8 bg-white/[.025] p-4 lg:block">
            <Lightbulb className="mb-3 size-4 text-[#ff9b70]" /><p className="text-xs font-medium text-white/80">好内容，从分享开始</p><p className="mt-2 text-xs leading-6 text-white/40">记录灵感，分享热爱。让每一份创作都找到它的观众。</p>
          </div>
        </div>
      </aside>
      <main className="min-w-0 px-4 py-7 sm:px-7 lg:px-10 lg:py-9">{children}</main>
    </div>
  </>;
}

export function CreatorAccess({ loading }: { loading: boolean }) {
  const [open, setOpen] = useState(false);
  return <>
    <div className="creator-panel mt-7 flex min-h-80 flex-col items-center justify-center px-6 py-12 text-center">
      <div className="mb-5 grid size-16 place-items-center rounded-2xl border border-white/10 bg-white/5 text-[#ff786c]">{loading ? <LoaderCircle className="size-6 animate-spin" /> : <Clapperboard className="size-7" />}</div>
      <h2 className="text-xl font-semibold">{loading ? '正在准备你的工作台' : '让热爱，成为作品'}</h2>
      <p className="mt-3 max-w-sm text-sm leading-6 text-white/45">{loading ? '正在确认登录状态，请稍候。' : '登录后即可上传视频、找回草稿，管理你的每一份创作。'}</p>
      {!loading && <button type="button" onClick={() => setOpen(true)} className="creator-primary mt-6"><LogIn className="size-4" />登录并开始创作</button>}
    </div>
    <AuthDialog open={open} onOpenChange={setOpen} />
  </>;
}
