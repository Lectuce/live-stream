'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { AppHeader } from '@/components/app-header';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { OwnerVideo } from '@/lib/types';
import { canPublishVideo, isPublicVideo, videoStatus } from '@/lib/video-status';

function SubmissionForm({ apiBase, initial }: { apiBase: string; initial: OwnerVideo | null }) {
  const [video, setVideo] = useState(initial);
  const [title, setTitle] = useState(initial?.title ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const active = useRef(true);
  const inFlight = useRef(false);
  useEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);

  async function upload(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;
    setError('');
    if (!file || !title.trim()) { setError('请填写标题并选择视频'); return; }
    if (!file.name.toLowerCase().endsWith('.mp4') || file.size === 0 || file.size > 100 * 1024 * 1024) {
      setError('请选择非空且不超过 100 MiB 的 MP4'); return;
    }
    inFlight.current = true;
    setBusy(true);
    try {
      let draft = video;
      if (!draft) {
        setMessage('正在创建草稿…');
        const result = await api.createVideo(apiBase, title.trim(), description.trim());
        if (!active.current) return;
        draft = { ...result.data, has_media: false };
        setVideo(draft);
        // Preserve the draft address even if uploading or the response fails.
        window.history.replaceState(window.history.state, '', `/upload?video_id=${encodeURIComponent(draft.video_id)}`);
      }
      setMessage('正在上传并检查视频，请保持页面打开…');
      const result = await api.uploadVideo(apiBase, draft.video_id, file);
      if (!active.current) return;
      // Upload returns only state and media metadata, not title/description.
      setVideo({ ...draft, process_status: result.data.process_status, audit_status: result.data.audit_status,
        publish_status: result.data.publish_status, duration_ms: result.data.duration_ms,
        play_url: result.data.play_url, has_media: true });
      setMessage('上传完成');
    } catch (err) {
      if (active.current) {
        setError(err instanceof Error ? err.message : '上传失败');
        setMessage('上传结果不确定时，请先刷新投稿状态，再决定是否重试。');
      }
    } finally {
      inFlight.current = false;
      if (active.current) setBusy(false);
    }
  }

  async function refreshOrPublish(publish = false) {
    if (!video || inFlight.current) return;
    inFlight.current = true;
    setBusy(true); setError(''); setMessage('');
    try {
      if (publish) {
        await api.publishVideo(apiBase, video.video_id);
        if (!active.current) return;
        setMessage('发布成功');
      }
      const result = await api.myVideoDetail(apiBase, video.video_id);
      if (active.current) setVideo(result.data);
    } catch (err) {
      if (active.current) setError(err instanceof Error ? err.message : '操作失败，请刷新状态后重试');
    } finally {
      inFlight.current = false;
      if (active.current) setBusy(false);
    }
  }

  const canUpload = !video || (video.publish_status === 'draft' && !video.has_media);
  return <form onSubmit={(event) => void upload(event)} className="mt-8 space-y-6 rounded-2xl border border-white/10 bg-white/[.03] p-6">
    {video && <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-sm text-[#ff8d80]">{videoStatus(video)} · 投稿 #{video.video_id}</p>
      <button type="button" disabled={busy} onClick={() => void refreshOrPublish()} className="text-sm text-white/60 disabled:opacity-40">刷新投稿状态</button>
    </div>}
    <label className="block space-y-2">
      <span>标题</span>
      <input required maxLength={100} value={title} disabled={busy || !!video} onChange={event => setTitle(event.target.value)} className="w-full rounded-lg border border-white/15 bg-black/30 p-3 disabled:opacity-60" />
    </label>
    <label className="block space-y-2">
      <span>简介</span>
      <textarea maxLength={5000} rows={4} value={description} disabled={busy || !!video} onChange={event => setDescription(event.target.value)} className="w-full rounded-lg border border-white/15 bg-black/30 p-3 disabled:opacity-60" />
    </label>
    {canUpload ? <label className="block space-y-2">
      <span>视频文件</span>
      <input type="file" accept=".mp4,video/mp4" required disabled={busy} onChange={event => setFile(event.target.files?.[0] ?? null)} className="block w-full text-sm file:mr-4 file:rounded-md file:border-0 file:bg-white/10 file:px-4 file:py-2 file:text-white" />
    </label> : <p className="text-sm text-white/50">文件已保存，无需再次上传。</p>}
    {video && <p className="text-xs text-white/40">投稿已保存，可随时从创作中心返回。当前版本暂不支持修改已保存的标题和简介。</p>}
    {video?.process_status === 'ready' && video.audit_status === 'pending' && <p className="text-sm text-amber-200">文件已就绪，审核通过后才能发布。当前版本尚未接入审核后台，等待或刷新不会自动通过审核。</p>}
    {video?.audit_status === 'rejected' && <p className="text-sm text-amber-200">审核未通过，当前版本尚未支持修改后重新提交。</p>}
    {message && <output className="block text-emerald-300">{message}</output>}
    {error && <p role="alert" className="text-red-300">{error}</p>}
    <div className="flex flex-wrap gap-3">
      {canUpload && <button type="submit" disabled={busy} className="rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold disabled:opacity-50">{busy ? '正在处理…' : video ? '继续上传' : '上传视频'}</button>}
      {video && canPublishVideo(video) && <button type="button" disabled={busy} onClick={() => void refreshOrPublish(true)} className="rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold disabled:opacity-50">{busy ? '正在处理…' : '发布视频'}</button>}
      {video && isPublicVideo(video) && <Link href={`/video/${video.video_id}`} className="rounded-lg bg-[#ff4d3d] px-6 py-3 font-semibold">查看视频</Link>}
    </div>
  </form>;
}

function RestoreSubmission({ apiBase, videoID }: { apiBase: string; videoID: string }) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ attempt: number; video?: OwnerVideo; error?: string } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void api.myVideoDetail(apiBase, videoID, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) setResult({ attempt, video: data });
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setResult({ attempt, error: error instanceof Error ? error.message : '无法恢复投稿' });
    });
    return () => controller.abort();
  }, [apiBase, videoID, attempt]);
  if (!result || result.attempt !== attempt) return <p className="mt-8">正在恢复投稿…</p>;
  if (!result.video) return <div className="mt-8 space-y-4">
    <p role="alert" className="text-red-300">{result.error}</p>
    <p className="text-sm text-white/50">请确认投稿属于当前账号。加载失败不会创建新的草稿。</p>
    <button type="button" onClick={() => setAttempt(value => value + 1)}>重试加载</button>
  </div>;
  return <SubmissionForm apiBase={apiBase} initial={result.video} />;
}

export function UploadPageContent({ videoID }: { videoID: string }) {
  const { user, loading, apiBase } = useAuth();
  const formKey = `${apiBase}:${user?.user_id}:${videoID}`;
  return <>
    <AppHeader />
    <main className="mx-auto max-w-3xl px-4 py-10">
      <Link href="/studio" className="text-sm text-white/50">返回创作中心</Link>
      <h1 className="mt-4 text-3xl font-bold">{videoID ? '我的投稿' : '发布你的作品'}</h1>
      <p className="mt-3 text-sm leading-6 text-white/50">支持 MP4（H.264 视频、AAC 音频或无音轨），最高 1080p，最长 1 小时，文件不超过 100 MiB。</p>
      {loading ? <p className="mt-6">正在检查登录状态…</p> : !user ? <p className="mt-6">请先点击右上角登录，再查看或上传自己的视频。</p> : videoID ?
        <RestoreSubmission key={formKey} apiBase={apiBase} videoID={videoID} /> :
        <SubmissionForm key={formKey} apiBase={apiBase} initial={null} />}
    </main>
  </>;
}
