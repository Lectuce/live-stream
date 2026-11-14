'use client';

import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { CreatorAccess } from '@/components/creator-shell';
import { useAuth } from '@/components/auth-provider';
import { PostsList } from '@/components/posts-list';

export default function MyPostsPage() {
  const { user, loading } = useAuth();
  return <><AppHeader /><main className="mx-auto max-w-3xl px-4 py-8"><div className="mb-7 flex items-center justify-between"><h1 className="text-2xl font-bold">我的图文</h1><Link className="creator-primary" href="/posts/new">发布图文</Link></div>{loading || !user ? <CreatorAccess loading={loading} /> : <PostsList />}</main></>;
}
