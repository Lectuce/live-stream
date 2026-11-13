'use client';

import { useRef, useState } from 'react';
import { ArrowDownToLine, LoaderCircle, Trash2 } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { api, ApiError } from '@/lib/api';
import type { OwnerVideo } from '@/lib/types';

export function VideoManageActions({ video, apiBase, disabled, onChanged }: {
  video: OwnerVideo; apiBase: string; disabled?: boolean;
  onChanged: (video: OwnerVideo | null) => void;
}) {
  const [action, setAction] = useState<'delete' | 'unpublish' | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const running = useRef(false);
  async function confirm() {
    if (!action || running.current) return;
    running.current = true; setBusy(true); setError('');
    try {
      if (action === 'delete') {
        await api.deleteVideo(apiBase, video.video_id);
        onChanged(null);
      } else {
        const result = await api.unpublishVideo(apiBase, video.video_id);
        onChanged(result.data);
      }
      setAction(null);
    } catch (err) {
      // A successful delete with a lost response can safely be retried.
      if (action === 'delete' && err instanceof ApiError && err.status === 404) {
        onChanged(null); setAction(null);
      } else setError(err instanceof Error ? err.message : '操作失败，请重试');
    } finally { running.current = false; setBusy(false); }
  }
  return <>
    <div className="flex flex-wrap justify-center gap-4 pt-2">
      {video.publish_status === 'published' && <button type="button" disabled={disabled || busy} onClick={() => { setError(''); setAction('unpublish'); }} className="inline-flex items-center gap-1.5 text-xs text-white/45 hover:text-white disabled:opacity-30"><ArrowDownToLine className="size-3.5" />下架</button>}
      <button type="button" disabled={disabled || busy} onClick={() => { setError(''); setAction('delete'); }} className="inline-flex items-center gap-1.5 text-xs text-red-300/65 hover:text-red-300 disabled:opacity-30"><Trash2 className="size-3.5" />删除</button>
    </div>
    <Dialog open={!!action} onOpenChange={open => { if (!busy && !open) setAction(null); }}>
      <DialogContent className="border-white/10 bg-[#11151d] sm:max-w-md">
        <DialogHeader><DialogTitle>{action === 'delete' ? '删除这条稿件？' : '下架这条视频？'}</DialogTitle><DialogDescription className="break-words leading-6">{video.title}<br />{action === 'delete' ? '删除后，稿件会从创作中心和公开视频中移除，当前暂不支持自助恢复。' : '下架后观众将无法继续通过公开页面访问，稿件保留为草稿，可编辑后再次发布。'}</DialogDescription></DialogHeader>
        {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
        <div className="mt-3 flex justify-end gap-3"><button type="button" disabled={busy} onClick={() => setAction(null)} className="creator-secondary">取消</button><button type="button" disabled={busy} onClick={() => void confirm()} className="creator-primary">{busy && <LoaderCircle className="size-4 animate-spin" />}{busy ? '正在处理…' : action === 'delete' ? '确认删除' : '确认下架'}</button></div>
      </DialogContent>
    </Dialog>
  </>;
}
