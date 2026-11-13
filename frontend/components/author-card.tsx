'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useAuth } from '@/components/auth-provider';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { FollowButton } from '@/components/follow-button';
import { api } from '@/lib/api';
import type { PublicUser } from '@/lib/types';

export function AuthorCard({ userId }: { userId: string }) {
  const { apiBase } = useAuth();
  return <AuthorContent key={`${apiBase}:${userId}`} base={apiBase} userId={userId} />;
}

function AuthorContent({ base, userId }: { base: string; userId: string }) {
  const [author, setAuthor] = useState<PublicUser | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void api.publicUser(base, userId, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) setAuthor(data);
    }).catch(() => { /* The author page remains accessible if this optional card fails. */ });
    return () => controller.abort();
  }, [base, userId]);
  return <div className="mt-4 flex flex-wrap items-center justify-between gap-4 rounded-xl border border-white/8 bg-white/[0.03] p-4">
    <Link href={`/space/${userId}`} className="flex min-w-0 items-center gap-3">
      <Avatar><AvatarImage src={author?.avatar_url || undefined} /><AvatarFallback>{author?.user_name.slice(0, 1) || 'UP'}</AvatarFallback></Avatar>
      <div className="min-w-0"><p className="truncate font-semibold hover:text-[#ff6b5e]">{author?.user_name || `作者 #${userId}`}</p>
        <p className="mt-1 text-xs text-white/45">{author ? `${author.follower_count} 粉丝 · 查看主页` : '查看作者主页'}</p></div>
    </Link>
    <FollowButton userId={userId} onChange={(state) => setAuthor((value) => value ? { ...value, follower_count: state.follower_count } : value)} />
  </div>;
}
