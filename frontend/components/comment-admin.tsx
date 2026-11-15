'use client';

import Link from 'next/link';
import { useEffect, useRef, useState } from 'react';
import { CreatorAccess, CreatorShell } from '@/components/creator-shell';
import { useAuth } from '@/components/auth-provider';
import { api } from '@/lib/api';
import type { CommentPage, CommentReport, CommentReview, ContentComment, Page } from '@/lib/types';

const labels: Record<string, string> = { pending: '待审核', approved: '已公开', rejected: '未通过', hidden: '已隐藏', edited: '作者编辑', deleted: '作者删除', resolved: '已处理', dismissed: '已驳回' };

function History({ id }: { id: string }) {
  const { apiBase } = useAuth();
  const [cursor, setCursor] = useState('');
  const [retry, setRetry] = useState(0);
  const [result, setResult] = useState<{key:string; data?:Page<CommentReview>;error?:string} | null>(null);
  const key = `${cursor}:${retry}`;
  useEffect(() => {
    const controller = new AbortController();
    void api.commentHistory(apiBase,id,cursor,controller.signal).then(({data}) => {if(!controller.signal.aborted)setResult({key,data});}).catch((e:unknown) => {if(!controller.signal.aborted)setResult({key,error:e instanceof Error?e.message:'读取失败'});});
    return () => controller.abort();
  },[apiBase,id,cursor,key]);
  const current = result?.key === key ? result : null;
  return <div className="mt-4 rounded-xl bg-black/20 p-4 text-xs text-white/50"><p className="mb-3 font-semibold text-white/80">操作记录</p>{!current ? '正在加载…' : current.error ? <p role="alert">{current.error}<button onClick={() => setRetry(v=>v+1)} className="ml-3 underline">重试</button></p> : <>{current.data?.items.map(item => <p key={item.review_id} className="mb-3">{new Date(item.created_at).toLocaleString('zh-CN')} · 操作人 #{item.actor_id} · 版本 {item.version} · {labels[item.action] || item.action}{item.reason && ` · ${item.reason}`}</p>)}{current.data?.items.length === 0 && <p>暂无操作记录</p>}<div className="mt-3 flex gap-3"><button disabled={!cursor} onClick={()=>setCursor('')}>最新记录</button><button disabled={!current.data?.has_more} onClick={()=>setCursor(current.data?.next_cursor || '')}>更早记录</button></div></>}</div>;
}

function ReviewActions({ comment, onDone }: { comment: Pick<ContentComment,'comment_id'|'version'|'audit_status'>; onDone:()=>void }) {
  const { apiBase } = useAuth();
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState(false);
  const [stale, setStale] = useState(false);
  const pending = useRef(false);
  async function review(decision:string) {
    if(pending.current || stale) return;
    if(decision !== 'approved' && !reason.trim()){setError('拒绝或隐藏时必须填写理由');return;}
    pending.current=true;setBusy(true);setError('');
    try {await api.reviewComment(apiBase,comment.comment_id,comment.version,decision,reason.trim());onDone();}
    catch(e){setError(e instanceof Error?e.message:'审核失败');setStale(true);}
    finally{pending.current=false;setBusy(false);}
  }
  return <div className="mt-4"><label className="block text-xs text-white/45">审核理由<textarea aria-label={`评论 ${comment.comment_id} 审核理由`} rows={2} maxLength={1000} className="creator-input mt-2 w-full" value={reason} disabled={busy} onChange={e=>setReason(e.target.value)} /></label><div className="mt-3 flex flex-wrap gap-2">{(['approved','rejected','hidden'] as const).map(decision=><button key={decision} type="button" className={decision==='approved'?'creator-primary':'creator-secondary'} disabled={busy || stale || decision === comment.audit_status} onClick={()=>void review(decision)}>{decision==='approved'?'通过 / 恢复':decision==='rejected'?'拒绝':'隐藏'}</button>)}<button className="creator-secondary" onClick={()=>setHistory(v=>!v)}>{history?'收起记录':'操作记录'}</button></div>{error&&<p role="alert" className="mt-3 text-sm text-red-300">{error}，请刷新列表后重试。</p>}{history&&<History id={comment.comment_id}/>}</div>;
}

function ReviewCard({item,onDone}:{item:ContentComment;onDone:()=>void}) {
  return <article className="creator-panel p-5"><div className="flex flex-wrap justify-between gap-2 text-xs text-white/45"><span>#{item.comment_id} · {item.user_name || `用户 #${item.user_id}`} · 版本 {item.version} · {labels[item.audit_status]}</span><Link href={`/${item.target_type==='video'?'video':'posts'}/${item.target_id}`} className="text-[#ff786c]">查看所属内容</Link></div><p className="mt-4 whitespace-pre-wrap break-words text-sm leading-7">{item.content}</p>{item.root_id!=='0'&&<p className="mt-2 text-xs text-white/40">回复评论 #{item.parent_id}，属于讨论 #{item.root_id}</p>}{item.audit_reason&&<p className="mt-3 text-sm text-[#ff9b70]">上次理由：{item.audit_reason}</p>}<ReviewActions comment={item} onDone={onDone}/></article>;
}

function ReportCard({item,onDone}:{item:CommentReport;onDone:()=>void}) {
  const {apiBase}=useAuth();
  const [reason,setReason]=useState('');const [busy,setBusy]=useState(false);const [error,setError]=useState('');const pending=useRef(false);
  async function resolve(status:string) {
    if(pending.current || !reason.trim())return;pending.current=true;setBusy(true);setError('');
    try{await api.resolveCommentReport(apiBase,item.report_id,status,reason.trim());onDone();}catch(e){setError(e instanceof Error?e.message:'处理失败');}finally{pending.current=false;setBusy(false);}
  }
  return <article className="creator-panel p-5"><p className="text-xs text-white/45">举报 #{item.report_id} · 举报人 #{item.user_id} · 评论 #{item.comment_id} · {item.status==='pending'?'待处理':labels[item.status]}</p><p className="mt-4 text-sm text-[#ff9b70]">举报理由：{item.reason}</p><blockquote className="mt-4 whitespace-pre-wrap break-words border-l-2 border-white/20 pl-4 text-sm leading-7 text-white/70">{item.content || '评论正文已删除'}</blockquote><ReviewActions comment={{comment_id:item.comment_id,version:item.comment_version,audit_status:item.audit_status as ContentComment['audit_status']}} onDone={onDone}/>{item.status==='pending'?<div className="mt-5 border-t border-white/10 pt-4"><label className="text-xs text-white/45">处理说明<textarea aria-label={`举报 ${item.report_id} 处理说明`} className="creator-input mt-2 w-full" maxLength={1000} value={reason} onChange={e=>setReason(e.target.value)} /></label><p className="my-2 text-xs text-white/40">确认违规时请先隐藏评论，再记录处理结果。</p><div className="flex gap-2"><button className="creator-primary" disabled={busy || !reason.trim()} onClick={()=>void resolve('resolved')}>标记已处理</button><button className="creator-secondary" disabled={busy || !reason.trim()} onClick={()=>void resolve('dismissed')}>驳回举报</button></div></div>:<p className="mt-4 text-sm text-white/50">处理人 #{item.resolved_by}：{item.resolution}</p>}{error&&<p role="alert" className="mt-3 text-red-300">{error}</p>}</article>;
}

function Workspace({reports}:{reports:boolean}) {
  const {apiBase}=useAuth();const [status,setStatus]=useState('pending');const [cursor,setCursor]=useState('');const [revision,setRevision]=useState(0);
  const [result,setResult]=useState<{key:string;comments?:CommentPage;reports?:Page<CommentReport>;error?:string}|null>(null);
  const key=`${status}:${cursor}:${revision}`;
  useEffect(()=>{
    const controller=new AbortController();
    const task=reports?api.commentReports(apiBase,status,cursor,controller.signal).then(({data})=>{if(!controller.signal.aborted)setResult({key,reports:data});}):api.reviewComments(apiBase,status,cursor,controller.signal).then(({data})=>{if(!controller.signal.aborted)setResult({key,comments:data});});
    void task.catch((e:unknown)=>{if(!controller.signal.aborted)setResult({key,error:e instanceof Error?e.message:'加载失败'});});
    return()=>controller.abort();
  },[apiBase,reports,status,cursor,key]);
  const current=result?.key===key?result:null;const page=current?.comments||current?.reports;
  function refresh(){setRevision(v=>v+1);}
  return <><div className="my-6 flex flex-wrap gap-2">{(reports?['pending','resolved','dismissed']:['pending','approved','rejected','hidden']).map(value=><button key={value} aria-pressed={value===status} className={value===status?'creator-primary':'creator-secondary'} onClick={()=>{setStatus(value);setCursor('');}}>{reports&&value==='pending'?'待处理':labels[value]}</button>)}<button className="creator-secondary" onClick={refresh}>刷新列表</button></div>{!current?<p className="py-8 text-white/40">正在加载…</p>:current.error?<p role="alert" className="text-red-300">{current.error}</p>:<><div className="space-y-5">{current.comments?.items.map(item=><ReviewCard key={`${item.comment_id}:${revision}:${item.version}`} item={item} onDone={refresh}/>)}{current.reports?.items.map(item=><ReportCard key={`${item.report_id}:${revision}:${item.comment_version}`} item={item} onDone={refresh}/>)}{page?.items.length===0&&<p className="creator-panel p-10 text-center text-white/40">当前没有待展示的内容</p>}</div><div className="mt-6 flex gap-3"><button className="creator-secondary" disabled={!cursor} onClick={()=>setCursor('')}>第一页</button><button className="creator-secondary" disabled={!page?.has_more} onClick={()=>setCursor(page?.next_cursor||'')}>下一页</button></div></>}</>;
}

export function CommentAdmin({reports=false}:{reports?:boolean}) {
  const {user,loading,apiBase}=useAuth();
  return <CreatorShell active={reports?'reports':'comments'}><h1 className="text-2xl font-bold">{reports?'评论举报':'评论审核'}</h1><p className="mt-3 text-sm text-white/45">{reports?'核查用户反馈，记录处理结果。':'审核视频与图文评论。隐藏根评论后，该讨论的回复也不再公开。'}</p>{loading||!user?<CreatorAccess loading={loading}/>:user.role!=='admin'?<p className="creator-panel mt-8 p-8">当前账号没有管理员权限。</p>:<Workspace key={`${apiBase}:${user.user_id}:${reports}`} reports={reports}/>}</CreatorShell>;
}
