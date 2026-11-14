'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { AuthorCard } from '@/components/author-card';
import { useAuth } from '@/components/auth-provider';
import { PostBody } from '@/components/post-card';
import { videoDate } from '@/components/creator-video-ui';
import { api } from '@/lib/api';
import type { Post } from '@/lib/types';

export function PostDetail({ id }: { id: string }) {
  const { apiBase } = useAuth();
  return <><AppHeader /><DetailContent key={`${apiBase}:${id}`} base={apiBase} id={id} /></>;
}

function DetailContent({ base, id }: { base: string; id: string }) {
  const { user } = useAuth();
  const [post, setPost] = useState<Post | null>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void api.post(base, id, false, controller.signal).then(({ data }) => { if (!controller.signal.aborted) { setPost(data); setError(''); } })
      .catch((err: unknown) => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '加载失败'); });
    return () => controller.abort();
  }, [base, id, retry]);
  return <main className="mx-auto max-w-3xl px-4 py-8"><Link href="/dynamic" className="text-sm text-white/50">← 返回动态</Link>
    {error ? <div className="creator-panel mt-6 p-8"><p role="alert" className="text-red-300">{error}</p><button className="creator-secondary mt-4" onClick={() => setRetry((v) => v + 1)}>重新加载</button></div> : !post ? <p className="mt-8 text-white/50">正在加载图文…</p> : <>
      <AuthorCard userId={post.owner_id} />
      <article className="creator-panel mt-6 p-5 sm:p-8"><div className="mb-6 flex items-center justify-between text-xs text-white/40"><span>{videoDate(post.published_at || post.created_at)} 发布</span>{String(user?.user_id) === post.owner_id && <Link href={`/posts/new?post_id=${id}`} className="text-[#ff786c]">编辑 / 删除</Link>}</div><PostBody post={post} /></article>
    </>}
  </main>;
}
