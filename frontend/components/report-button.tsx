'use client';
import { useRef, useState } from 'react';
import { Flag } from 'lucide-react';
import { useAuth } from '@/components/auth-provider';
import { AuthDialog } from '@/components/auth-dialog';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import {
  interactions,
  interactionError,
  type TargetType,
} from '@/lib/interactions';

export function ReportButton({ kind, id }: { kind: TargetType; id: string }) {
  const { user, apiBase } = useAuth();
  const [open, setOpen] = useState(false),
    [login, setLogin] = useState(false);
  const [reason, setReason] = useState(''),
    [feedback, setFeedback] = useState(''),
    [busy, setBusy] = useState(false);
  const pending = useRef(false);
  async function submit() {
    if (pending.current || !reason.trim()) return;
    pending.current = true;
    setBusy(true);
    setFeedback('');
    try {
      await interactions.report(apiBase, kind, id, reason.trim());
      setFeedback('举报已提交，管理员会查看并处理。');
      setReason('');
    } catch (err) {
      setFeedback(interactionError(err));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <>
      <button
        type="button"
        aria-label="举报内容"
        className="inline-flex items-center gap-1 text-xs text-white/45 hover:text-white"
        onClick={() => (user ? setOpen(true) : setLogin(true))}
      >
        <Flag className="size-3.5" />
        举报
      </button>
      <AuthDialog open={login} onOpenChange={setLogin} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogTitle>举报内容</DialogTitle>
          <DialogDescription>
            请描述具体问题，帮助管理员判断。
          </DialogDescription>
          <textarea
            aria-label="举报理由"
            maxLength={1000}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="creator-input min-h-28"
          />
          {feedback && (
            <output className="text-sm text-[#ff9b89]">{feedback}</output>
          )}
          <button
            className="creator-primary"
            disabled={busy || !reason.trim()}
            onClick={() => void submit()}
          >
            {busy ? '正在提交…' : '提交举报'}
          </button>
        </DialogContent>
      </Dialog>
    </>
  );
}
