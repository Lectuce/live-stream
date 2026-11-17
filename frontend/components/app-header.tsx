'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useState, type ComponentType } from 'react';
import {
  Clapperboard,
  LogOut,
  Radio,
  RadioTower,
  Sparkles,
  Upload,
  UserRound,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { useAuth } from '@/components/auth-provider';
import { NotificationLink } from '@/components/notifications';
import { AuthDialog } from '@/components/auth-dialog';
import { cn } from '@/lib/utils';

interface HeaderLink {
  href: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  visible?: string;
  active: (pathname: string) => boolean;
}

const primaryLinks: HeaderLink[] = [
  {
    href: '/video',
    label: '视频',
    icon: Clapperboard,
    active: (pathname) =>
      pathname === '/' ||
      pathname.startsWith('/video') ||
      pathname.startsWith('/popular') ||
      pathname.startsWith('/category'),
  },
  {
    href: '/live',
    label: '直播',
    icon: RadioTower,
    active: (pathname) =>
      pathname.startsWith('/live') ||
      pathname.startsWith('/feed') ||
      pathname.startsWith('/rooms'),
  },
  {
    href: '/dynamic',
    label: '动态',
    icon: Sparkles,
    visible: 'hidden md:inline-flex',
    active: (pathname) => pathname.startsWith('/dynamic'),
  },
];

export function AppHeader() {
  const pathname = usePathname();
  const { user, loading, logout } = useAuth();
  const [authOpen, setAuthOpen] = useState(false);

  return (
    <>
      <header className="glass sticky top-0 z-40 border-b border-white/8">
        <div className="mx-auto flex h-16 max-w-[1480px] items-center gap-3 px-3 sm:gap-5 sm:px-6 lg:px-8">
          <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-4">
            <Link
              href="/"
              className="flex shrink-0 items-center gap-3"
              aria-label="返回视频首页"
            >
              <span className="grid size-9 place-items-center rounded-xl bg-[#ff4d3d] shadow-[0_0_25px_rgb(255_77_61/28%)]">
                <Radio className="size-5" />
              </span>
              <span className="hidden xl:block">
                <span className="block text-sm font-black tracking-[.16em]">
                  LIVE NOW
                </span>
                <span className="text-[10px] tracking-[.18em] text-white/35">
                  REALTIME STAGE
                </span>
              </span>
            </Link>

            <nav
              aria-label="内容导航"
              className="flex min-w-0 items-center gap-1 overflow-x-auto border-l border-white/8 pl-2 sm:pl-4"
            >
              {primaryLinks.map(({ href, label, icon: Icon, visible, active }) => {
                const selected = active(pathname);
                return (
                  <Link
                    key={href}
                    href={href}
                    aria-current={selected ? 'page' : undefined}
                    className={cn(
                      visible ?? 'inline-flex',
                      'h-9 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-xs font-semibold transition sm:px-3',
                      selected
                        ? 'bg-white/8 text-white'
                        : 'text-white/55 hover:bg-white/5 hover:text-white',
                    )}
                  >
                    <Icon
                      className={cn(
                        'size-4',
                        selected && 'text-[#ff786c]',
                      )}
                    />
                    {label}
                  </Link>
                );
              })}
            </nav>
          </div>

          <nav
            aria-label="个人导航"
            className="flex shrink-0 items-center gap-1 sm:gap-2"
          >
            {user && (
              <>
                <Link
                  href="/studio"
                  className="hidden h-9 items-center rounded-lg px-3 text-xs font-semibold text-white/55 transition hover:bg-white/5 hover:text-white lg:inline-flex"
                >
                  创作中心
                </Link>
                <Link
                  href="/upload"
                  className="hidden h-9 items-center gap-1.5 rounded-lg bg-[#ff4d3d] px-3 text-xs font-semibold text-white transition hover:bg-[#ff6356] sm:inline-flex"
                >
                  <Upload className="size-3.5" />
                  投稿
                </Link>
                <NotificationLink />
              </>
            )}

            {!loading && user ? (
              <>
                <Link
                  href={'/space/' + user.user_id}
                  aria-label="进入我的主页"
                  className="flex items-center gap-2 rounded-full border border-white/8 bg-white/4 p-1 transition hover:border-white/15 hover:bg-white/10 sm:pr-3"
                >
                  <Avatar size="sm">
                    <AvatarImage src={user.avatar_url || undefined} />
                    <AvatarFallback>
                      {user.user_name.slice(0, 1).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <span className="hidden max-w-24 truncate text-xs text-white/70 sm:inline">
                    {user.user_name}
                  </span>
                </Link>
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-white/45 hover:text-white"
                  onClick={() => void logout()}
                  aria-label="退出登录"
                >
                  <LogOut />
                </Button>
              </>
            ) : (
              !loading && (
                <Button
                  size="sm"
                  onClick={() => setAuthOpen(true)}
                  className="bg-white text-black hover:bg-white/85"
                >
                  <UserRound />
                  登录
                </Button>
              )
            )}
          </nav>
        </div>
      </header>
      <AuthDialog open={authOpen} onOpenChange={setAuthOpen} />
    </>
  );
}
