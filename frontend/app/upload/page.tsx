'use client';

import { useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { Video } from '@/lib/types';

export default function UploadPage() {
  const { user, loading, apiBase } = useAuth();
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [draftID, setDraftID] = useState('');
  const [uploaded, setUploaded] = useState<Video | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [published, setPublished] = useState(false);

  async function upload(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setError('');
    if (!file || !title.trim()) { setError('请填写标题并选择视频'); return; }
    if (!file.name.toLowerCase().endsWith('.mp4') || file.size === 0 || file.size > 100 * 1024 * 1024) {
      setError('请选择非空且不超过 100 MiB 的 MP4'); return;
    }
    setBusy(true);
    try {
      let id = draftID;
      if (!id) {
        setMessage('正在创建草稿…');
        const result = await api.createVideo(apiBase, title.trim(), description.trim());
        id = result.data.video_id;
        setDraftID(id);
      }
      setMessage('正在上传并检查视频，请保持页面打开…');
      const result = await api.uploadVideo(apiBase, id, file);
      setUploaded(result.data);
      setMessage(result.data.audit_status === 'approved' ? '视频已就绪，可以发布' : '上传完成，等待审核');
    } catch (err) {
      setError(err instanceof Error ? err.message : '上传失败');
      setMessage('');
    } finally { setBusy(false); }
  }

  async function publish() {
    if (!uploaded || busy) return;
    setBusy(true); setError('');
    try {
      await api.publishVideo(apiBase, uploaded.video_id);
      setPublished(true);
      setMessage('发布成功');
    } catch (err) { setError(err instanceof Error ? err.message : '发布失败'); }
    finally { setBusy(false); }
  }

  return (
    <>
      <AppHeader />
      <main className="mx-auto max-w-3xl px-4 py-10">
        <Link href="/video" className="text-sm text-white/50">返回视频列表</Link>
        <h1 className="mt-4 text-3xl font-bold">发布你的作品</h1>
        <p className="mt-3 text-sm leading-6 text-white/50">支持 MP4（H.264 视频、AAC 音频或无音轨），最高 1080p，最长 1 小时，文件不超过 100 MiB。</p>
        {loading ? <p className="mt-6">正在检查登录状态…</p> : !user ? (
          <p className="mt-6">请先点击右上角登录，再上传视频。</p>
        ) : (
          <form onSubmit={(event) => void upload(event)} className="mt-8 space-y-6 rounded-2xl border border-white/10 bg-white/[.03] p-6">
            <label className="block space-y-2">
              <span>标题</span>
              <input required maxLength={100} value={title} disabled={busy || !!draftID} onChange={(event) => setTitle(event.target.value)} className="w-full rounded-lg border border-white/15 bg-black/30 p-3 disabled:opacity-60" />
            </label>
            <label className="block space-y-2">
              <span>简介</span>
              <textarea maxLength={5000} rows={4} value={description} disabled={busy || !!draftID} onChange={(event) => setDescription(event.target.value)} className="w-full rounded-lg border border-white/15 bg-black/30 p-3 disabled:opacity-60" />
            </label>
            <label className="block space-y-2">
              <span>视频文件</span>
              <input type="file" accept=".mp4,video/mp4" required disabled={busy || !!uploaded} onChange={(event) => setFile(event.target.files?.[0] ?? null)} className="block w-full text-sm file:mr-4 file:rounded-md file:border-0 file:bg-white/10 file:px-4 file:py-2 file:text-white" />
            </label>
            {draftID && <p className="text-xs text-white/40">草稿 #{draftID} · 上传失败时可更换文件后重试；标题修改将在创作中心支持。</p>}
            {message && <output className="block text-emerald-300">{message}</output>}
            {error && <p role="alert" className="text-red-300">{error}</p>}
            {!uploaded && <button type="submit" disabled={busy} className="rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold disabled:opacity-50">{busy ? '正在处理…' : draftID ? '重试上传' : '上传视频'}</button>}
            {uploaded && !published && uploaded.audit_status === 'approved' && <button type="button" disabled={busy} onClick={() => void publish()} className="rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold disabled:opacity-50">{busy ? '正在发布…' : '发布视频'}</button>}
            {published && <Link href={`/video/${draftID}`} className="inline-block rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold">查看视频</Link>}
          </form>
        )}
      </main>
    </>
  );
}
