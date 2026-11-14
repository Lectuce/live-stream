'use client';

import Link from 'next/link';
import Image from 'next/image';
import type { Post } from '@/lib/types';
import { videoDate } from '@/components/creator-video-ui';

export function PostBody({ post, compact = false }: { post: Post; compact?: boolean }) {
  return <>
    <p className={`${compact ? 'line-clamp-5' : ''} whitespace-pre-wrap break-words text-sm leading-7 text-white/80`}>{post.content}</p>
    {post.images.length > 0 && <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3">{post.images.map((image, index) => <a key={image.image_id} href={image.url} target="_blank" rel="noreferrer" aria-label={`查看图片 ${index + 1}`} className="overflow-hidden rounded-lg border border-white/8 bg-black/20"><Image unoptimized width={600} height={600} src={image.url} alt={`图文配图 ${index + 1}`} loading="lazy" className="aspect-square w-full object-cover transition hover:opacity-80" /></a>)}</div>}
  </>;
}

export function PostCard({ post, mine = false }: { post: Post; mine?: boolean }) {
  return <article className="creator-panel p-5 sm:p-6">
    <div className="mb-4 flex items-center justify-between gap-3 text-xs text-white/40"><span>{videoDate(post.published_at || post.created_at)} · {post.status === 'published' ? '已发布' : '草稿'}</span><Link className="text-[#ff786c] hover:text-white" href={mine ? `/posts/new?post_id=${post.post_id}` : `/posts/${post.post_id}`}>{mine ? '编辑图文' : '查看全文'}</Link></div>
    <PostBody post={post} compact />
  </article>;
}
