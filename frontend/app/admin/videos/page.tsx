'use client';

import { useEffect, useRef, useState } from 'react';
import { CheckCircle2, ChevronRight, LoaderCircle, RefreshCw, ShieldCheck, ShieldX } from 'lucide-react';
import { CreatorAccess, CreatorShell } from '@/components/creator-shell';
import { VideoStatusBadge, videoDate } from '@/components/creator-video-ui';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { ReviewDetail, ReviewPage } from '@/lib/types';

function ReviewPanel({ base, id, onReviewed }: { base: string; id: string; onReviewed: () => void }) {
  const [result, setResult] = useState<{ data?: ReviewDetail; error?: string } | null>(null);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState('');
  const [mediaReady, setMediaReady] = useState(false);
  const [mediaError, setMediaError] = useState(false);
  const inFlight = useRef(false);
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    const controller = new AbortController();
    void api.reviewDetail(base, id, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) setResult({ data });
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setResult({ error: err instanceof Error ? err.message : '读取失败' });
    });
    return () => { active.current = false; controller.abort(); };
  }, [base, id]);

  async function submit(decision: 'approved' | 'rejected') {
    const video = result?.data;
    if (!video || inFlight.current || stale) return;
    if (decision === 'rejected' && !reason.trim()) { setError('请填写拒绝原因'); return; }
    inFlight.current = true; setBusy(true); setError('');
    try {
      await api.submitReview(base, id, video.content_version, decision, reason.trim());
      if (active.current) onReviewed();
    } catch (err) {
      if (active.current) {
        setError(err instanceof Error ? err.message : '提交失败');
        // A failed response may follow a committed transaction. Reload before retrying.
        setStale(true);
      }
    } finally { inFlight.current = false; if (active.current) setBusy(false); }
  }

  if (!result) return <div className="creator-panel grid min-h-80 place-items-center"><LoaderCircle className="size-6 animate-spin text-[#ff786c]" /></div>;
  if (!result.data) return <div role="alert" className="creator-panel p-8 text-sm text-red-300">{result.error}，请点击上方刷新后重试。</div>;
  const video = result.data;
  const reviewable = video.audit_status === 'pending' && video.process_status === 'ready' && video.publish_status === 'draft' && video.has_media;
  const mediaURL = `${base}/api/v1/admin/videos/${encodeURIComponent(id)}/media?version=${encodeURIComponent(video.content_version)}`;
  return <section className="creator-panel min-w-0 overflow-hidden">
    <div className="border-b border-white/8 p-5"><div className="flex flex-wrap items-center justify-between gap-3"><VideoStatusBadge video={video} /><span className="text-xs text-white/35">内容版本 {video.content_version}</span></div><h2 className="mt-4 break-words text-lg font-semibold">{video.title}</h2><p className="mt-2 text-xs text-white/40">作者：{video.owner_username || `#${video.owner_id}`} · 提交于 {videoDate(video.submitted_at || video.created_at)}</p></div>
    <div className="space-y-5 p-5">
      {video.has_media && <video key={mediaURL} src={mediaURL} crossOrigin="use-credentials" controls playsInline preload="metadata" onLoadedData={() => { setMediaReady(true); setMediaError(false); }} onError={() => { setMediaReady(false); setMediaError(true); }} className="aspect-video w-full rounded-xl bg-black"><track kind="captions" src="data:text/vtt,WEBVTT" srcLang="zh" label="暂无字幕" /></video>}
      {mediaError && <p role="alert" className="text-xs text-red-300">预览加载失败，权限或稿件状态可能已变化。请刷新后重试。</p>}
      <div><h3 className="text-xs text-white/40">视频简介</h3><p className="mt-2 whitespace-pre-wrap break-words text-sm leading-7 text-white/70">{video.description || '暂无简介'}</p></div>
      {video.audit_reason && <p className="rounded-lg bg-red-400/5 p-3 text-sm text-red-300">拒绝原因：{video.audit_reason}</p>}
      {reviewable ? <div className="space-y-4 border-t border-white/8 pt-5">
        <label className="block text-sm">审核意见<span className="ml-2 text-xs text-white/35">拒绝时必填，最多 1000 字</span><textarea value={reason} maxLength={1000} disabled={busy || stale} onChange={event => setReason(event.target.value)} rows={3} placeholder="请说明具体问题，帮助作者修改作品…" className="creator-input mt-3 resize-y" /></label>
        {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
        {stale && <p className="text-xs text-amber-200">为避免重复或过期审核，请点击上方刷新，重新查看稿件状态。</p>}
        <div className="flex flex-wrap gap-3"><button type="button" disabled={busy || stale || !mediaReady} onClick={() => void submit('approved')} className="creator-primary"><CheckCircle2 className="size-4" />{busy ? '提交中…' : '审核通过'}</button><button type="button" disabled={busy || stale || !reason.trim()} onClick={() => void submit('rejected')} className="creator-secondary"><ShieldX className="size-4" />拒绝并反馈</button></div>
        <p className="text-xs text-white/35">请检查视频及文字内容后再决定。通过审核后，由作者自行发布。</p>
      </div> : <p className="rounded-lg bg-white/4 p-3 text-xs text-white/45">该稿件当前无需审核。内容变更后会重新进入待审核队列。</p>}
      <div className="border-t border-white/8 pt-5"><h3 className="text-sm font-semibold">最近审核记录</h3>{!video.reviews.length ? <p className="mt-3 text-xs text-white/35">暂无人工审核记录</p> : <ul className="mt-3 space-y-3">{video.reviews.map(review => <li key={review.review_id} className="rounded-lg bg-white/[.025] p-3 text-xs"><div className="flex flex-wrap justify-between gap-2 text-white/60"><span>版本 {review.content_version} · {review.decision === 'approved' ? '通过' : '拒绝'}</span><span>{videoDate(review.created_at)} · 审核员 #{review.reviewer_id}</span></div>{review.reason && <p className="mt-2 whitespace-pre-wrap break-words leading-6 text-white/45">{review.reason}</p>}</li>)}</ul>}</div>
    </div>
  </section>;
}

function ReviewWorkspace({ base }: { base: string }) {
  const [status, setStatus] = useState('pending');
  const [cursor, setCursor] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState('');
  const [notice, setNotice] = useState('');
  const [result, setResult] = useState<{ key: string; data?: ReviewPage; error?: string } | null>(null);
  const key = `${status}:${cursor}:${refresh}`;
  useEffect(() => {
    const controller = new AbortController();
    void api.reviewList(base, status, cursor, controller.signal).then(({ data }) => {
      if (!controller.signal.aborted) setResult({ key, data });
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setResult({ key, error: err instanceof Error ? err.message : '加载失败' });
    });
    return () => controller.abort();
  }, [base, status, cursor, key]);
  const current = result?.key === key ? result : null;
  return <>
    <div className="mt-7 flex flex-wrap items-center justify-between gap-3"><div className="flex gap-1 rounded-lg bg-white/5 p-1">{[['pending', '待审核'], ['approved', '已通过'], ['rejected', '已拒绝']].map(([value, label]) => <button type="button" key={value} onClick={() => { setStatus(value); setCursor(''); setSelected(''); setNotice(''); }} className={`rounded-md px-4 py-2 text-sm ${status === value ? 'bg-[#ff4d3d]/15 text-[#ff786c]' : 'text-white/45 hover:text-white'}`}>{label}</button>)}</div><button type="button" onClick={() => { setRefresh(v => v + 1); setNotice(''); }} className="creator-secondary"><RefreshCw className="size-4" />刷新</button></div>
    {notice && <output className="mt-4 block text-sm text-emerald-300">{notice}</output>}
    <div className="mt-5 grid items-start gap-5 xl:grid-cols-[320px_minmax(0,1fr)]">
      <section className="creator-panel overflow-hidden" aria-label="审核队列"><div className="border-b border-white/8 p-4 text-xs text-white/45">审核队列 · 本页 {current?.data?.items.length ?? '—'} 条</div>{!current ? <p className="p-8 text-sm text-white/40">正在加载…</p> : current.error ? <p role="alert" className="p-5 text-sm text-red-300">{current.error}</p> : <><div className="divide-y divide-white/6">{current.data?.items.map(video => <button key={video.video_id} type="button" onClick={() => setSelected(video.video_id)} aria-pressed={selected === video.video_id} className={`block w-full p-4 text-left transition hover:bg-white/5 ${selected === video.video_id ? 'border-l-2 border-[#ff4d3d] bg-[#ff4d3d]/5' : ''}`}><p className="line-clamp-2 break-words text-sm font-medium leading-6">{video.title}</p><p className="mt-2 text-xs text-white/40">{video.owner_username || `作者 #${video.owner_id}`} · 版本 {video.content_version}</p><p className="mt-2 text-[11px] text-white/25">提交 {videoDate(video.submitted_at || video.created_at)}</p></button>)}{current.data?.items.length === 0 && <div className="px-5 py-12 text-center"><ShieldCheck className="mx-auto mb-3 size-8 text-white/20" /><p className="text-sm text-white/45">当前没有此状态的稿件</p></div>}</div><div className="flex justify-between gap-2 border-t border-white/8 p-3"><button type="button" disabled={!cursor} onClick={() => { setCursor(''); setSelected(''); }} className="creator-secondary px-3 text-xs">第一页</button><button type="button" disabled={!current.data?.has_more} onClick={() => { setCursor(current.data?.next_cursor || ''); setSelected(''); }} className="creator-secondary px-3 text-xs">下一页<ChevronRight className="size-3" /></button></div></>}</section>
      {selected ? <ReviewPanel key={`${selected}:${refresh}`} base={base} id={selected} onReviewed={() => { setSelected(''); setRefresh(v => v + 1); setNotice('审核结果已保存'); }} /> : <div className="creator-panel flex min-h-80 flex-col items-center justify-center gap-4 p-6 text-center"><ShieldCheck className="size-10 text-[#ff786c]/60" /><h2 className="font-semibold">选择一条稿件开始审核</h2><p className="max-w-xs text-xs leading-6 text-white/40">预览视频、查看文字信息，为作者提供明确的审核结果。</p></div>}
    </div>
  </>;
}

export default function AdminVideosPage() {
  const { user, loading, apiBase } = useAuth();
  return <CreatorShell active="review"><p className="mb-2 text-[10px] font-semibold tracking-[.2em] text-[#ff786c]">CONTENT REVIEW</p><h1 className="text-2xl font-bold sm:text-3xl">视频审核</h1><p className="mt-2 text-sm text-white/40">认真对待每一份创作，让内容安心被看见。</p>{loading || !user ? <CreatorAccess loading={loading} /> : user.role !== 'admin' ? <div className="creator-panel mt-7 p-10 text-center"><ShieldX className="mx-auto mb-4 size-8 text-red-300" /><h2 className="font-semibold">无管理员权限</h2><p className="mt-3 text-sm text-white/40">当前账号无法访问审核后台。</p></div> : <ReviewWorkspace key={`${apiBase}:${user.user_id}`} base={apiBase} />}</CreatorShell>;
}
