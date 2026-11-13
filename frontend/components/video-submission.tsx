'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { VideoManageActions } from '@/components/video-manage-actions';
import { ArrowLeft, ArrowRight, Check, CheckCircle2, CircleHelp, FileVideo, Film, Info, LoaderCircle, RefreshCw, Send, UploadCloud } from 'lucide-react';
import { CreatorAccess, CreatorShell } from '@/components/creator-shell';
import { VideoStatusBadge, videoDuration } from '@/components/creator-video-ui';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { OwnerVideo } from '@/lib/types';
import { canPublishVideo, isPublicVideo } from '@/lib/video-status';

function SubmissionForm({ apiBase, initial }: { apiBase: string; initial: OwnerVideo | null }) {
  const router = useRouter();
  const [video, setVideo] = useState(initial);
  const [title, setTitle] = useState(initial?.title ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState('');
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const active = useRef(true);
  const inFlight = useRef(false);
  const dirty = !!video && (title.trim() !== video.title || description.trim() !== video.description);

  async function saveMetadata() {
    if (!video || inFlight.current) return;
    inFlight.current = true; setBusy(true); setError(''); setMessage('');
    try {
      const result = await api.editVideo(apiBase, video.video_id, title, description);
      if (active.current) {
        setVideo(result.data); setTitle(result.data.title); setDescription(result.data.description);
        setMessage(result.data.audit_status === 'pending' && result.data.has_media ? '修改已保存，稿件需要重新审核' : '修改已保存');
      }
    } catch (err) { if (active.current) setError(err instanceof Error ? err.message : '保存失败'); }
    finally { inFlight.current = false; if (active.current) setBusy(false); }
  }
  useEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);

  useEffect(() => {
    if (preview) return () => URL.revokeObjectURL(preview);
  }, [preview]);

  function chooseFile(next: File | null) {
    if (!next) return;
    setDragging(false);
    if (!next.name.toLowerCase().endsWith('.mp4') || next.size === 0 || next.size > 100 * 1024 * 1024) {
      setError('请选择非空且不超过 100 MiB 的 MP4 视频');
      return;
    }
    setError('');
    setFile(next);
    setPreview(URL.createObjectURL(next));
  }

  async function upload(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;
    if (dirty) { setError('请先保存标题和简介，再上传视频'); return; }
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
    if (dirty) { setError('请先保存修改，再刷新或发布'); return; }
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
  const hasFile = !!video?.has_media || !!file;
  return <form onSubmit={(event) => void upload(event)} className="mt-7">
    <ol aria-label="投稿进度" className="mb-7 flex items-center gap-2 sm:gap-4">
      {['选择视频', '填写信息', '审核与发布'].map((step, index) => {
        const complete = index === 0 ? hasFile : index === 1 ? !!video : !!video && isPublicVideo(video);
        const current = index === 0 ? !hasFile : index === 1 ? hasFile && !video : !!video;
        return <li key={step} className="flex flex-1 items-center gap-2 sm:gap-3"><span className={`grid size-6 shrink-0 place-items-center rounded-full text-[10px] font-semibold ${complete ? 'bg-[#ff4d3d]/15 text-[#ff786c]' : current ? 'bg-[#ff4d3d] text-white' : 'bg-white/5 text-white/30'}`}>{complete ? <Check className="size-3.5" /> : `0${index + 1}`}</span><span className={`whitespace-nowrap text-xs ${current || complete ? 'text-white/80' : 'text-white/30'}`}>{step}</span>{index < 2 && <span className="ml-1 hidden h-px flex-1 bg-white/10 sm:block" />}</li>;
      })}
    </ol>
    <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_280px]">
      <div className="min-w-0 space-y-5">
        <section className="creator-panel overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/8 px-5 py-4 sm:px-6"><h2 className="flex items-center gap-2.5 text-sm font-semibold"><Film className="size-4 text-[#ff786c]" />视频文件</h2><span className="text-[10px] tracking-wider text-white/30">MP4 · MAX 100 MiB</span></div>
          <div className="p-5 sm:p-6">
            {canUpload && !file ? <label className={`group relative flex min-h-60 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed px-5 py-8 text-center transition ${dragging ? 'border-[#ff786c] bg-[#ff4d3d]/10' : 'border-white/15 bg-[#090c12]/60 hover:border-[#ff4d3d]/50 hover:bg-[#ff4d3d]/[.035]'} ${busy ? 'pointer-events-none opacity-50' : ''}`}>
              <input type="file" aria-label="选择或拖入 MP4 视频" accept=".mp4,video/mp4" disabled={busy} onDragEnter={() => setDragging(true)} onDragLeave={() => setDragging(false)} onDrop={() => setDragging(false)} onChange={event => { chooseFile(event.target.files?.[0] ?? null); event.target.value = ''; }} className="absolute inset-0 z-10 h-full w-full cursor-pointer rounded-xl opacity-0 focus-visible:opacity-100" />
              <div className="mb-5 grid size-16 place-items-center rounded-2xl border border-[#ff4d3d]/15 bg-[#ff4d3d]/8 text-[#ff786c] transition group-hover:-translate-y-1"><UploadCloud className="size-8" strokeWidth={1.4} /></div>
              <p className="text-sm font-semibold">点击选择视频，或将文件拖到这里</p><p className="mt-2 text-xs text-white/35">让你的灵感，有画面，也有声音。</p><span className="creator-primary mt-5 px-6 py-2">选择视频</span>
            </label> : file && canUpload ? <div className="overflow-hidden rounded-xl border border-white/10 bg-[#090c12]">
              {preview && <video key={preview} src={preview} controls playsInline preload="metadata" className="aspect-video max-h-64 w-full bg-black"><track kind="captions" src="data:text/vtt,WEBVTT" srcLang="zh" label="暂无字幕" /></video>}
              <div className="flex items-center gap-3 p-4"><FileVideo className="size-5 shrink-0 text-[#ff786c]" /><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{file.name}</p><p className="mt-1 text-xs text-white/35">{(file.size / 1024 / 1024).toFixed(1)} MiB · 本地预览，尚未上传</p></div><label className={`relative shrink-0 cursor-pointer text-xs text-white/60 hover:text-white ${busy ? 'pointer-events-none opacity-40' : ''}`}>更换文件<input type="file" aria-label="更换视频文件" accept=".mp4,video/mp4" disabled={busy} onChange={event => { chooseFile(event.target.files?.[0] ?? null); event.target.value = ''; }} className="absolute inset-0 w-full cursor-pointer opacity-0" /></label></div>
            </div> : <div className="flex flex-col items-center gap-4 rounded-xl border border-emerald-400/10 bg-emerald-400/[.035] px-5 py-8 text-center sm:flex-row sm:text-left"><div className="grid size-14 shrink-0 place-items-center rounded-xl bg-emerald-400/10 text-emerald-300"><CheckCircle2 className="size-7" strokeWidth={1.5} /></div><div><h3 className="text-sm font-semibold">视频文件已保存</h3><p className="mt-2 text-xs text-white/40">{video?.duration_ms ? `时长 ${videoDuration(video.duration_ms)} · ` : ''}无需重复上传，可以继续查看投稿状态。</p></div></div>}
            <p className="mt-4 flex items-start gap-2 text-[11px] leading-5 text-white/35"><Info className="mt-0.5 size-3.5 shrink-0" />支持 H.264 视频与 AAC 音频，最高 1080p、最长 1 小时。</p>
          </div>
        </section>
        <section className="creator-panel overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/8 px-5 py-4 sm:px-6"><h2 className="text-sm font-semibold">基本信息</h2><span className="text-xs text-white/30">让观众更快发现你的作品</span></div>
          <div className="space-y-6 p-5 sm:p-6">
            <label htmlFor="submission-title" aria-label="视频标题" className="block"><span className="mb-2.5 flex items-center justify-between text-sm"><span>视频标题 <span className="text-[#ff786c]">*</span></span><span className="text-[11px] tabular-nums text-white/30">{title.length} / 100</span></span><input id="submission-title" required maxLength={100} placeholder="给作品起一个让人想点开的名字" value={title} disabled={busy || (!!video && video.publish_status !== 'draft')} onChange={event => setTitle(event.target.value)} className="creator-input" /></label>
            <label htmlFor="submission-description" aria-label="视频简介" className="block"><span className="mb-2.5 flex items-center justify-between text-sm"><span>视频简介 <span className="ml-1 text-xs text-white/30">选填</span></span><span className="text-[11px] tabular-nums text-white/30">{description.length} / 5000</span></span><textarea id="submission-description" maxLength={5000} rows={5} placeholder="聊聊视频的内容、创作灵感，或者你想对观众说的话…" value={description} disabled={busy || (!!video && video.publish_status !== 'draft')} onChange={event => setDescription(event.target.value)} className="creator-input min-h-32 resize-y leading-6" /></label>
            {video && <div className="space-y-3"><p className="flex items-start gap-2 text-xs leading-5 text-white/40"><Info className="mt-0.5 size-3.5 shrink-0" />{video.publish_status === 'published' ? '已发布视频请先下架，再修改信息。' : '修改后请保存；正式审核模式下，内容变更需要重新审核。'}</p>{video.publish_status === 'draft' && <button type="button" onClick={() => void saveMetadata()} disabled={busy || !dirty || !title.trim()} className="creator-secondary">{busy ? '正在处理…' : dirty ? '保存修改' : '已保存'}</button>}</div>}
          </div>
        </section>
      </div>
      <aside className="space-y-5 xl:sticky xl:top-24">
        <section className="creator-panel p-5">
          <h2 className="flex items-center gap-2 text-sm font-semibold"><Send className="size-4 text-[#ff786c]" />{video && isPublicVideo(video) ? '作品已发布' : '准备好与大家见面了吗？'}</h2>
          <p className="mt-3 text-xs leading-6 text-white/40">完善信息并上传视频，审核通过后即可发布。</p>
          <div className="my-5 space-y-3 border-y border-white/8 py-4 text-xs">{[['视频文件', hasFile], ['视频标题', !!title.trim()], ['上传完成', !!video?.has_media]].map(([label, done]) => <div key={String(label)} className="flex items-center justify-between"><span className="text-white/50">{label}</span>{done ? <CheckCircle2 className="size-3.5 text-emerald-300" /> : <span className="text-white/25">待完成</span>}</div>)}</div>
          {video && <div className="mb-4 space-y-3"><div className="flex items-center justify-between"><VideoStatusBadge video={video} /><button type="button" disabled={busy} onClick={() => void refreshOrPublish()} className="inline-flex items-center gap-1 text-[11px] text-white/40 hover:text-white disabled:opacity-40"><RefreshCw className={`size-3 ${busy ? 'animate-spin' : ''}`} />刷新</button></div><p className="break-all text-[10px] text-white/25">稿件 ID · {video.video_id}</p></div>}
          {video?.process_status === 'ready' && video.audit_status === 'pending' && <p className="mb-4 rounded-lg border border-amber-300/10 bg-amber-300/5 p-3 text-xs leading-6 text-amber-200/80">视频已上传，审核通过后才能发布。目前暂未开放审核处理，稿件会保留在创作中心。</p>}
          {video?.audit_status === 'rejected' && <p className="mb-4 rounded-lg bg-red-400/5 p-3 text-xs leading-6 text-red-300">审核未通过，可修改标题和简介后保存，重新进入待审核。</p>}
          {message && <output className="mb-4 flex items-start gap-2 text-xs leading-6 text-emerald-300">{busy && <LoaderCircle className="mt-1 size-3.5 shrink-0 animate-spin" />}{message}</output>}
          {error && <p role="alert" className="mb-4 rounded-lg border border-red-400/15 bg-red-400/5 p-3 text-xs leading-6 text-red-300">{error}</p>}
          <div className="space-y-3">
            {dirty && <p className="text-xs text-amber-200">有未保存的修改，请先点击“保存修改”。</p>}
            {canUpload && <button type="submit" disabled={busy || !file || dirty} className="creator-primary w-full">{busy ? <LoaderCircle className="size-4 animate-spin" /> : <UploadCloud className="size-4" />}{busy ? '正在处理…' : video ? '继续上传' : '上传视频'}</button>}
            {video && canPublishVideo(video) && <button type="button" disabled={busy || dirty} onClick={() => void refreshOrPublish(true)} className="creator-primary w-full"><Send className="size-4" />{busy ? '正在发布…' : '发布视频'}</button>}
            {video && isPublicVideo(video) && <Link href={`/video/${video.video_id}`} className="creator-primary w-full">查看作品<ArrowRight className="size-4" /></Link>}
            <Link href="/studio" className="creator-secondary w-full">返回稿件管理</Link>
            {video && <VideoManageActions video={video} apiBase={apiBase} disabled={busy || dirty} onChanged={next => {
              if (!next) { router.replace('/studio'); return; }
              setVideo(next); setTitle(next.title); setDescription(next.description); setMessage('已下架，可修改信息或再次发布'); setError('');
            }} />}
          </div>
          <p className="mt-4 text-center text-[10px] leading-5 text-white/30">{busy ? '上传期间请保持页面打开' : video ? '草稿已保存，可从创作中心继续' : '请上传你有权分享的视频内容'}</p>
        </section>
        <section className="px-1"><h2 className="flex items-center gap-2 text-xs font-medium text-white/60"><CircleHelp className="size-3.5" />投稿小贴士</h2><ul className="mt-3 space-y-2.5 pl-5 text-xs leading-6 text-white/35"><li className="list-disc">清晰的标题，让内容更容易被发现。</li><li className="list-disc">用简介补充背景，给观众更多共鸣。</li><li className="list-disc">上传中断后，可从创作中心找回稿件。</li></ul></section>
      </aside>
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
  if (!result || result.attempt !== attempt) return <div className="creator-panel mt-7 flex min-h-80 flex-col items-center justify-center gap-4"><LoaderCircle className="size-6 animate-spin text-[#ff786c]" /><p className="text-sm text-white/50">正在找回你的作品…</p></div>;
  if (!result.video) return <div className="creator-panel mt-7 space-y-4 px-6 py-16 text-center">
    <p role="alert" className="text-sm text-red-300">{result.error}</p>
    <p className="text-xs text-white/40">请确认投稿属于当前账号，或稍后重新加载。</p>
    <button type="button" onClick={() => setAttempt(value => value + 1)} className="creator-secondary"><RefreshCw className="size-4" />重试加载</button>
  </div>;
  return <SubmissionForm apiBase={apiBase} initial={result.video} />;
}

export function UploadPageContent({ videoID }: { videoID: string }) {
  const { user, loading, apiBase } = useAuth();
  const formKey = `${apiBase}:${user?.user_id}:${videoID}`;
  return <CreatorShell active="upload">
      <div className="flex flex-wrap items-center justify-between gap-4"><div><p className="mb-2 text-[10px] font-semibold tracking-[.2em] text-[#ff786c]">CREATE SOMETHING WORTH SHARING</p><h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{videoID ? '管理视频投稿' : '发布视频'}</h1><p className="mt-2 text-sm text-white/40">把灵感变成作品，把热爱分享给更多人。</p></div><Link href="/studio" className="inline-flex items-center gap-1.5 text-xs text-white/40 hover:text-white"><ArrowLeft className="size-3.5" />返回稿件管理</Link></div>
      {loading || !user ? <CreatorAccess loading={loading} /> : videoID ?
        <RestoreSubmission key={formKey} apiBase={apiBase} videoID={videoID} /> :
        <SubmissionForm key={formKey} apiBase={apiBase} initial={null} />}
  </CreatorShell>;
}
