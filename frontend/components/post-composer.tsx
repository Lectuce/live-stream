'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { ImagePlus, LoaderCircle, Send, Trash2, X } from 'lucide-react';
import { AppHeader } from '@/components/app-header';
import { CreatorAccess } from '@/components/creator-shell';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { Post, PostImage } from '@/lib/types';

export function PostComposer({ id }: { id: string }) {
  const { user, loading, apiBase } = useAuth();
  return <><AppHeader /><main className="mx-auto max-w-3xl px-4 py-8">
    <Link href="/posts" className="text-sm text-white/50">← 我的图文</Link>
    <div className="mt-6 mb-7"><p className="text-xs tracking-widest text-[#ff786c]">SHARE A MOMENT</p><h1 className="mt-2 text-3xl font-bold">{id ? '编辑图文' : '分享此刻'}</h1><p className="mt-3 text-sm text-white/45">用文字和图片，记录值得分享的事。</p></div>
    {loading || !user ? <CreatorAccess loading={loading} /> : <Composer key={`${apiBase}:${user.user_id}:${id}`} base={apiBase} initialID={id} />}
  </main></>;
}

function Composer({ base, initialID }: { base: string; initialID: string }) {
  const [post, setPost] = useState<Post | null>(null);
  const [content, setContent] = useState('');
  const [images, setImages] = useState<PostImage[]>([]);
  const [loading, setLoading] = useState(!!initialID);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loadFailed, setLoadFailed] = useState(false);
  const pending = useRef(false);
  const currentPost = useRef<Post | null>(null);
  const lifetime = useRef<AbortController | null>(null);

  function remember(data: Post) { currentPost.current = data; setPost(data); }
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    if (initialID) void api.post(base, initialID, true, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) { remember(data); setContent(data.content); setImages(data.images); }
    }).catch((err: unknown) => { if (!controller.signal.aborted) { setError(err instanceof Error ? err.message : '草稿加载失败'); setLoadFailed(true); } })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [base, initialID]);

  async function draft() {
    if (currentPost.current) return currentPost.current;
    const { data } = await api.createPost(base, lifetime.current?.signal);
    if (lifetime.current?.signal.aborted) throw new Error('请求已取消');
    remember(data);
    window.history.replaceState(null, '', `/posts/new?post_id=${data.post_id}`);
    return data;
  }
  async function save() {
    const previous = await draft();
    const { data } = await api.editPost(base, previous.post_id, previous.version, content, images.map((image) => image.image_id), lifetime.current?.signal);
    if (lifetime.current?.signal.aborted) throw new Error('请求已取消');
    remember(data); setImages(data.images); return data;
  }
  async function action(run: () => Promise<void>) {
    if (pending.current || loading || loadFailed) return;
    pending.current = true; setBusy(true); setError(''); setNotice('');
    try { await run(); } catch (err) { if (!lifetime.current?.signal.aborted) setError(err instanceof Error ? err.message : '操作失败'); }
    finally { pending.current = false; if (!lifetime.current?.signal.aborted) setBusy(false); }
  }
  async function upload(files: File[]) {
    if (files.length + images.length > 9) throw new Error('每条图文最多9张图片');
    if (files.some((file) => file.size > 2 * 1024 * 1024)) throw new Error('每张图片不能超过2MiB');
    await draft();
    for (const file of files) {
      const { data } = await api.uploadPostImage(base, file, lifetime.current?.signal);
      if (lifetime.current?.signal.aborted) return;
      setImages((old) => [...old, data]);
    }
    setNotice('图片已上传，请保存草稿或发布。');
  }

  if (loading) return <p className="py-12 text-white/50">正在加载图文…</p>;
  return <div className="creator-panel p-5 sm:p-7">
    <div className="mb-5 flex items-center justify-between"><span className="rounded-md bg-white/5 px-2.5 py-1 text-xs text-white/50">{post?.status === 'published' ? '编辑已发布图文' : '图文草稿'}</span><span className="text-xs text-white/35">文字 / 图片</span></div>
    <label htmlFor="post-content" className="mb-3 block text-sm font-medium">想分享什么？</label>
    <textarea id="post-content" value={content} disabled={busy || loadFailed} onChange={(event) => setContent(event.target.value)} maxLength={5000} rows={9} placeholder="写下你的想法、近况，或一段创作背后的故事…" className="creator-input w-full resize-y leading-7" />
    <p className="mt-2 text-right text-xs tabular-nums text-white/30">{content.length} / 5000</p>
    {images.length > 0 && <div className="mt-4 grid grid-cols-3 gap-3">{images.map((image, index) => <div key={image.image_id} className="relative overflow-hidden rounded-xl border border-white/10"><Image unoptimized width={600} height={600} src={image.url} alt={`待发布图片 ${index + 1}`} className="aspect-square w-full object-cover" /><button type="button" disabled={busy} className="absolute top-1 right-1 rounded-full bg-black/70 p-1.5 hover:bg-red-500" aria-label={`移除图片 ${index + 1}`} onClick={() => setImages((old) => old.filter((item) => item.image_id !== image.image_id))}><X className="size-3.5" /></button></div>)}</div>}
    <div className="mt-5 flex flex-wrap items-center gap-3"><label className={`creator-secondary ${busy || loadFailed || images.length >= 9 ? 'pointer-events-none opacity-40' : 'cursor-pointer'}`}><ImagePlus className="size-4" />添加图片<input type="file" className="sr-only" accept="image/jpeg,image/png" multiple disabled={busy || loadFailed || images.length >= 9} onChange={(event) => { const files = Array.from(event.target.files || []); event.target.value = ''; if (files.length) void action(() => upload(files)); }} /></label><span className="text-xs text-white/35">JPEG / PNG，单张 ≤ 2MiB，最多9张</span></div>
    {error && <div role="alert" className="mt-5 rounded-lg bg-red-400/10 p-3 text-sm text-red-300"><p>{error}</p>{post && <p className="mt-2 text-xs">如提示版本冲突，请先复制未保存的正文，再重新加载。</p>}<button className="mt-2 underline" onClick={() => window.location.reload()}>重新加载</button></div>}
    {notice && <output className="mt-5 block text-sm text-emerald-300">{notice}</output>}
    <div className="mt-7 flex flex-wrap items-center justify-between gap-3 border-t border-white/8 pt-5">
      <div>{post && <button type="button" disabled={busy || loadFailed} className="inline-flex items-center gap-1.5 text-xs text-white/40 hover:text-red-300" onClick={() => { if (window.confirm('确定删除这条图文？删除后将无法通过公开链接访问。')) void action(async () => { await api.deletePost(base, post.post_id); if (!lifetime.current?.signal.aborted) window.location.assign('/posts'); }); }}><Trash2 className="size-3.5" />删除图文</button>}</div>
      <div className="flex gap-3"><button type="button" disabled={busy || loadFailed} className="creator-secondary" onClick={() => void action(async () => { await save(); setNotice(post?.status === 'published' ? '修改已保存' : '草稿已保存'); })}>{busy && <LoaderCircle className="size-4 animate-spin" />}{post?.status === 'published' ? '保存修改' : '保存草稿'}</button>
        {post?.status !== 'published' ? <button type="button" disabled={busy || loadFailed || (!content.trim() && !images.length)} className="creator-primary" onClick={() => void action(async () => { const saved = await save(); const { data } = await api.publishPost(base, saved.post_id, saved.version, lifetime.current?.signal); if (!lifetime.current?.signal.aborted) window.location.assign(`/posts/${data.post_id}`); })}><Send className="size-4" />发布图文</button> : <Link href={`/posts/${post.post_id}`} className="creator-primary">查看图文</Link>}
      </div>
    </div>
  </div>;
}
