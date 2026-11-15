'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { LoaderCircle, MessageSquare, RefreshCw } from 'lucide-react';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { api, ApiError } from '@/lib/api';
import type { CommentPage, ContentComment, CommentTargetType } from '@/lib/types';

// getRandomValues also works on the project's HTTP development origin.
function requestID() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function ContentComments({ targetType, targetID }: { targetType: CommentTargetType; targetID: string }) {
  const { apiBase, user, loading } = useAuth();
  return <section className="mt-10 border-t border-white/10 pt-7" aria-label={targetType === 'video' ? '视频评论' : '图文评论'}>
    <div className="mb-6 flex items-center gap-2"><MessageSquare className="size-5 text-[#ff786c]" /><h2 className="text-xl font-semibold">评论</h2></div>
    <CommentsContent key={`${apiBase}:${targetType}:${targetID}:${user?.user_id ?? 'guest'}:${loading}`} targetType={targetType} targetID={targetID} />
  </section>;
}

function CommentsContent({ targetType, targetID }: { targetType: CommentTargetType; targetID: string }) {
  const { user } = useAuth();
  const [revision, setRevision] = useState(0);
  const [sort, setSort] = useState<'latest' | 'hot' | 'mine'>('latest');
  const [notice, setNotice] = useState('');
  function sent() { setNotice('已提交，审核通过后公开。可在“我的评论”查看进度。'); setSort('mine'); setRevision(v => v + 1); }
  return <>
    <CommentComposer targetType={targetType} targetID={targetID} onSent={sent} />
    {notice && <output className="mt-3 block text-sm text-[#ff9b70]">{notice}</output>}
    <div className="my-5 flex flex-wrap items-center justify-between gap-3"><div className="flex gap-2">{(['latest', 'hot', ...(user ? ['mine'] : [])] as const).map(value => <button key={value} type="button" aria-pressed={sort === value} className={sort === value ? 'creator-primary' : 'creator-secondary'} onClick={() => setSort(value as typeof sort)}>{value === 'latest' ? '最新' : value === 'hot' ? '热门' : '我的评论'}</button>)}</div><button type="button" className="flex items-center gap-1.5 text-xs text-white/45 hover:text-white" onClick={() => setRevision((value) => value + 1)}><RefreshCw className="size-3" />刷新评论</button></div>
    <CommentList key={`${sort}:${revision}`} targetType={targetType} targetID={targetID} sort={sort} onSent={sent} />
  </>;
}

function CommentComposer({ targetType, targetID, parentID = '0', label = '说说你的看法…', onSent, onCancel }: {
  targetType: CommentTargetType; targetID: string; parentID?: string; label?: string; onSent: () => void; onCancel?: () => void;
}) {
  const { apiBase, user, loading } = useAuth();
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (parentID !== '0') { input.current?.focus(); input.current?.scrollIntoView({block:'nearest'}); } }, [parentID]);
  const [content, setContent] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef(false);
  const command = useRef<{ id: string; text: string; parent: string } | null>(null);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);

  async function send() {
    const text = content.trim();
    if (!user || !text || pending.current) return;
    pending.current = true; setBusy(true); setError('');
    const abort = new AbortController(); controller.current = abort;
    try {
      if (!command.current || command.current.text !== text || command.current.parent !== parentID) command.current = { id: requestID(), text, parent: parentID };
      await api.createComment(apiBase, targetType, targetID, text, parentID, command.current.id, abort.signal);
      if (!abort.signal.aborted) { setContent(''); command.current = null; onSent(); }
    } catch (err) { if (!abort.signal.aborted) setError(err instanceof Error ? err.message : '发送失败，请重试'); }
    finally { pending.current = false; if (!abort.signal.aborted) setBusy(false); }
  }

  if (loading) return <p className="text-sm text-white/40">正在确认登录状态…</p>;
  if (!user) return <div className="rounded-xl border border-white/8 bg-white/[.025] p-5 text-center text-sm text-white/45"><button type="button" className="text-[#ff786c] hover:underline" onClick={() => setOpen(true)}>登录</button>后参与讨论<AuthDialog open={open} onOpenChange={setOpen} /></div>;
  return <form className="rounded-xl border border-white/8 bg-white/[.025] p-4" onSubmit={(event) => { event.preventDefault(); void send(); }}>
    <textarea ref={input} aria-label={label} placeholder={label} value={content} maxLength={2000} rows={3} disabled={busy} onChange={(event) => setContent(event.target.value)} className="creator-input w-full resize-y text-sm" />
    <div className="mt-3 flex items-center justify-between gap-3"><span className="text-xs text-white/30">{content.length} / 2000</span><div className="flex gap-2">{onCancel && <button type="button" disabled={busy} className="creator-secondary" onClick={onCancel}>取消</button>}<button type="submit" disabled={busy || !content.trim()} className="creator-primary">{busy && <LoaderCircle className="size-4 animate-spin" />}{busy ? '发送中…' : parentID === '0' ? '发表评论' : '发送回复'}</button></div></div>
    {error && <p role="alert" className="mt-3 text-sm text-red-300">{error}</p>}
  </form>;
}

function CommentList({ targetType, targetID, rootID = '', closed = false, onReply, sort = 'latest', onSent }: {
  targetType: CommentTargetType; targetID: string; rootID?: string; closed?: boolean; onReply?: (comment: ContentComment) => void; sort?: 'latest' | 'hot' | 'mine'; onSent?: () => void;
}) {
  const { apiBase } = useAuth();
  const [items, setItems] = useState<ContentComment[]>([]);
  const [page, setPage] = useState<CommentPage | null>(null);
  const [cursor, setCursor] = useState('');
  const [expired, setExpired] = useState(false);
  const [retry, setRetry] = useState(0);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void (sort === 'mine' ? api.myComments(apiBase, targetType, targetID, cursor, controller.signal) : api.comments(apiBase, targetType, targetID, rootID, cursor, controller.signal, sort)).then(({ data }) => {
      if (controller.signal.aborted) return;
      setError(''); setExpired(false); setPage(data);
      setItems((previous) => { const seen = new Set(previous.map((item) => item.comment_id)); return cursor ? [...previous, ...data.items.filter((item) => !seen.has(item.comment_id))] : data.items; });
    }).catch((err: unknown) => { if (!controller.signal.aborted) { setError(err instanceof Error ? err.message : '评论加载失败'); setExpired(err instanceof ApiError && (err.status === 410 || err.status === 400)); } })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
  }, [apiBase, targetType, targetID, rootID, cursor, retry, sort]);
  function deleted(id: string) { setItems((old) => old.map((item) => item.comment_id === id ? { ...item, content: '', deleted_at: new Date().toISOString() } : item)); }
  return <div className="space-y-5">
    {items.map((item) => rootID || sort === 'mine' ? <CommentRow key={`${item.comment_id}:${item.version}`} comment={item} closed={closed || sort === 'mine'} onDeleted={() => deleted(item.comment_id)} onEdited={() => { setCursor(''); setRetry(v => v + 1); onSent?.(); }} onReply={() => onReply?.(item)} /> : <CommentThread key={`${item.comment_id}:${item.version}`} comment={item} onDeleted={() => deleted(item.comment_id)} onSent={onSent} />)}
    {busy && <p className="flex items-center gap-2 py-3 text-sm text-white/40"><LoaderCircle className="size-4 animate-spin" />正在加载{rootID ? '回复' : '评论'}…</p>}
    {error && <div role="alert" className="text-sm text-red-300"><p>{error}</p><button type="button" className="creator-secondary mt-3" onClick={() => { setBusy(true); if (expired) { setCursor(''); setItems([]); } setRetry((value) => value + 1); }}>{expired ? '刷新评论' : '重试加载'}</button></div>}
    {!busy && !error && items.length === 0 && <p className="py-6 text-center text-sm text-white/35">{rootID ? '暂无回复' : '还没有评论，来聊聊你的看法吧。'}</p>}
    {page?.truncated && <p className="text-xs text-white/40">热门展示最多500条评论，切换“最新”可继续浏览全部评论。</p>}
    {!busy && !error && page?.has_more && <button type="button" className="creator-secondary" onClick={() => { setBusy(true); setCursor(page.next_cursor); }}>加载更多{rootID ? '回复' : '评论'}</button>}
  </div>;
}

function CommentThread({ comment, onDeleted, onSent }: { comment: ContentComment; onDeleted: () => void; onSent?: () => void }) {
  const [expanded, setExpanded] = useState(false);
  const [target, setTarget] = useState<ContentComment | null>(null);
  const [revision, setRevision] = useState(0);
  const [added, setAdded] = useState(0);
  const closed = !!comment.deleted_at;
  return <article className="border-b border-white/8 pb-6">
    <CommentRow comment={comment} closed={closed} onDeleted={onDeleted} onEdited={onSent} onReply={() => setTarget(comment)} />
    {(comment.reply_count > 0 || added > 0 || expanded) && <button type="button" className="mt-3 ml-11 text-xs text-white/50 hover:text-[#ff786c]" onClick={() => setExpanded((value) => !value)}>{expanded ? '收起回复' : `查看 ${comment.reply_count + added} 条回复`}</button>}
    {expanded && <div className="mt-4 ml-4 border-l border-white/10 pl-4 sm:ml-11"><CommentList key={revision} targetType={comment.target_type} targetID={comment.target_id} rootID={comment.comment_id} closed={closed} onReply={setTarget} onSent={onSent} /></div>}
    {target && !closed && <div className="mt-4 ml-4 sm:ml-11"><CommentComposer key={target.comment_id} targetType={comment.target_type} targetID={comment.target_id} parentID={target.comment_id} label={`回复 ${target.user_name}`} onCancel={() => setTarget(null)} onSent={() => { setTarget(null); setExpanded(true); setAdded((value) => value + 1); setRevision((value) => value + 1); onSent?.(); }} /></div>}
  </article>;
}

function CommentRow({ comment, closed, onDeleted, onReply, onEdited }: { comment: ContentComment; closed: boolean; onDeleted: () => void; onReply: () => void; onEdited?: () => void }) {
  const { apiBase, user } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [mode, setMode] = useState<'edit' | 'report' | null>(null);
  const [draft, setDraft] = useState(comment.content);
  const [reason, setReason] = useState('');
  const [liked, setLiked] = useState(comment.liked);
  const [count, setCount] = useState(comment.like_count);
  const [notice, setNotice] = useState('');
  const [loginOpen, setLoginOpen] = useState(false);
  const pending = useRef(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function remove() {
    if (pending.current || !window.confirm('确定删除这条评论？正文会被清除，已有回复将保留。')) return;
    pending.current = true; setBusy(true); setError('');
    const abort = new AbortController(); controller.current = abort;
    try { await api.deleteComment(apiBase, comment.target_type, comment.target_id, comment.comment_id, abort.signal); if (!abort.signal.aborted) onDeleted(); }
    catch (err) { if (!abort.signal.aborted) setError(err instanceof Error ? err.message : '删除失败'); }
    finally { pending.current = false; if (!abort.signal.aborted) setBusy(false); }
  }
  async function action(kind: 'like' | 'edit' | 'report') {
    if (!user) { setLoginOpen(true); return; }
    if (pending.current) return;
    pending.current = true; setBusy(true); setError('');
    try {
      if (kind === 'like') { const {data} = await api.likeComment(apiBase, comment.comment_id, !liked); setLiked(data.liked); setCount(data.like_count); }
      if (kind === 'edit') { await api.editComment(apiBase, comment.comment_id, comment.version, draft.trim()); setMode(null); setNotice('修改已提交审核'); onEdited?.(); }
      if (kind === 'report') { await api.reportComment(apiBase, comment.comment_id, reason.trim()); setMode(null); setNotice('举报已提交'); }
    } catch (err) { setError(err instanceof Error ? err.message : '操作失败'); }
    finally { pending.current = false; setBusy(false); }
  }
  return <div className="flex gap-3">
    <Link href={`/space/${comment.user_id}`} aria-label={`${comment.user_name}的主页`}><Avatar><AvatarImage src={comment.avatar_url || undefined} /><AvatarFallback>{comment.user_name.slice(0, 1)}</AvatarFallback></Avatar></Link>
    <div className="min-w-0 flex-1"><AuthDialog open={loginOpen} onOpenChange={setLoginOpen} /><Link href={`/space/${comment.user_id}`} className="text-sm font-medium text-white/65 hover:text-[#ff786c]">{comment.user_name}</Link>
      {comment.deleted_at ? <p className="mt-2 text-sm italic text-white/30">评论已删除</p> : <p className="mt-2 whitespace-pre-wrap break-words text-sm leading-7 text-white/85">{comment.parent_id !== '0' && comment.reply_to_user_name && <span className="mr-1 text-white/45">回复 {comment.reply_to_user_name}：</span>}{comment.content}</p>}
      <div className="mt-2 flex flex-wrap items-center gap-4 text-xs text-white/35"><time dateTime={comment.created_at}>{new Date(comment.created_at).toLocaleString('zh-CN')}</time>
        {!closed && !comment.deleted_at && comment.audit_status === 'approved' && <button type="button" onClick={onReply} className="hover:text-[#ff786c]">回复</button>}
        {!comment.deleted_at && String(user?.user_id) === comment.user_id && <button type="button" disabled={busy} onClick={() => void remove()} className="hover:text-red-300">{busy ? '删除中…' : '删除'}</button>}
        {!comment.deleted_at && comment.audit_status === 'approved' && <><button type="button" disabled={busy} aria-pressed={liked} onClick={() => void action('like')} className={liked ? 'text-[#ff786c]' : 'hover:text-[#ff786c]'}>{liked ? '已赞' : '点赞'} {count}</button><button type="button" disabled={busy} onClick={() => user ? setMode('report') : setLoginOpen(true)}>举报</button></>}
        {!comment.deleted_at && String(user?.user_id) === comment.user_id && <button type="button" disabled={busy} onClick={() => { setDraft(comment.content); setMode('edit'); }}>编辑</button>}
      </div>
      {!comment.deleted_at && String(user?.user_id) === comment.user_id && <p className="mt-2 text-xs text-white/45">{{pending:'待审核',approved:'已公开',rejected:'未通过',hidden:'已隐藏'}[comment.audit_status]}{comment.audit_reason ? ` · ${comment.audit_reason}` : ''}</p>}
      {mode && <form className="mt-3 space-y-3" onSubmit={e => {e.preventDefault(); void action(mode);}}><textarea aria-label={mode === 'edit' ? '修改评论' : '举报理由'} className="creator-input w-full" rows={3} maxLength={mode === 'edit' ? 2000 : 1000} value={mode === 'edit' ? draft : reason} onChange={e => mode === 'edit' ? setDraft(e.target.value) : setReason(e.target.value)} disabled={busy} /><p className="text-xs text-white/40">{mode === 'edit' ? '修改后重新审核，通过前不会公开。' : '请说明具体问题，管理员会核查处理。'}</p><div className="flex gap-2"><button className="creator-primary" disabled={busy || !(mode === 'edit' ? draft : reason).trim()}>{busy ? '提交中…' : '提交'}</button><button type="button" className="creator-secondary" disabled={busy} onClick={() => setMode(null)}>取消</button></div></form>}
      {notice && <output className="mt-2 block text-xs text-[#ff9b70]">{notice}</output>}
      {error && <p role="alert" className="mt-2 text-xs text-red-300">{error}</p>}
    </div>
  </div>;
}
