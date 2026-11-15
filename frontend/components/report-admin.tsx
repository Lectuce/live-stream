'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { ShieldCheck } from 'lucide-react';
import { CreatorShell, CreatorAccess } from '@/components/creator-shell';
import { useAuth } from '@/components/auth-provider';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import {
  interactions,
  interactionError,
  type Report,
  type ReportAction,
} from '@/lib/interactions';
import type { Page } from '@/lib/types';
const kinds: Record<string, string> = {
  '': '全部类型',
  video: '视频',
  post: '图文',
  comment: '评论',
  danmaku: '直播弹幕',
};
const actions: Record<string, string> = {
  hidden: '隐藏内容',
  restored: '恢复内容',
  resolved: '标记已处理',
  dismissed: '驳回举报',
};
export function ReportAdmin() {
  const { user, loading, apiBase } = useAuth();
  return (
    <CreatorShell active="reports">
      <h1 className="flex items-center gap-3 text-2xl font-semibold">
        <ShieldCheck className="text-[#ff786c]" />
        举报处理
      </h1>
      <p className="mt-3 text-sm text-white/45">
        核对内容、记录理由，再决定处理方式。
      </p>
      {!user ? (
        <CreatorAccess loading={loading} />
      ) : user.role !== 'admin' ? (
        <p className="mt-8 text-red-300">只有管理员可以查看举报。</p>
      ) : (
        <ReportQueue key={`${apiBase}:${user.user_id}`} />
      )}
    </CreatorShell>
  );
}
function ReportQueue() {
  const [status, setStatus] = useState('pending'),
    [kind, setKind] = useState('');
  return (
    <>
      <div className="my-6 flex flex-wrap gap-3">
        <select
          aria-label="举报状态"
          className="creator-input w-auto"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        >
          <option value="pending">待处理</option>
          <option value="resolved">已处理</option>
          <option value="dismissed">已驳回</option>
        </select>
        <select
          aria-label="举报类型"
          className="creator-input w-auto"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          {Object.entries(kinds).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
      </div>
      <Reports key={`${status}:${kind}`} status={status} kind={kind} />
    </>
  );
}
function Reports({ status, kind }: { status: string; kind: string }) {
  const { apiBase } = useAuth();
  const [page, setPage] = useState<Page<Report> | null>(null),
    [error, setError] = useState(''),
    [reload, setReload] = useState(0),
    [busy, setBusy] = useState(false),
    [selected, setSelected] = useState<Report | null>(null);
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .reports(apiBase, status, kind, '', c.signal)
      .then(({ data }) => {
        if (!c.signal.aborted) {
          setPage(data);
          setError('');
        }
      })
      .catch((err) => {
        if (!c.signal.aborted) setError(interactionError(err));
      });
    return () => c.abort();
  }, [apiBase, status, kind, reload]);
  async function more() {
    if (!page?.has_more || pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const { data } = await interactions.reports(
        apiBase,
        status,
        kind,
        page.next_cursor,
      );
      setPage((p) => ({
        ...data,
        items: [
          ...(p?.items || []),
          ...data.items.filter(
            (r) => !p?.items.some((v) => v.report_id === r.report_id),
          ),
        ],
      }));
    } catch (err) {
      setError(interactionError(err));
    } finally {
      setBusy(false);
      pending.current = false;
    }
  }
  return (
    <>
      {error && (
        <p role="alert" className="mb-4 text-red-300">
          {error}
        </p>
      )}
      <button
        className="creator-secondary mb-5"
        disabled={busy}
        onClick={() => setReload((v) => v + 1)}
      >
        刷新列表
      </button>
      {!page && !error && <p>正在读取举报…</p>}
      {page?.items.length === 0 && (
        <div className="creator-panel p-12 text-center text-white/40">
          暂无这类举报
        </div>
      )}
      <div className="space-y-4">
        {page?.items.map((r) => (
          <article key={r.report_id} className="creator-panel p-5">
            <div className="flex items-center justify-between gap-3">
              <span className="rounded-md bg-[#ff786c]/10 px-2 py-1 text-xs text-[#ff8a80]">
                {kinds[r.target_type]}
              </span>
              <time className="text-xs text-white/35">
                {new Date(r.created_at).toLocaleString('zh-CN')}
              </time>
            </div>
            <p className="mt-4 whitespace-pre-wrap break-words text-sm text-white/80">
              {r.preview || '内容已删除或不可用'}
            </p>
            <p className="mt-4 whitespace-pre-wrap break-words rounded-lg bg-white/4 p-3 text-sm text-white/55">
              举报理由：{r.reason}
            </p>
            {r.resolution && (
              <p className="mt-3 text-sm text-white/50">
                处理说明：{r.resolution}
              </p>
            )}
            <div className="mt-4 flex gap-4">
              <button
                className="creator-secondary"
                onClick={() => setSelected(r)}
              >
                查看并处理
              </button>
              {r.url && (
                <Link
                  href={r.url}
                  className="self-center text-xs text-[#ff8a80]"
                >
                  打开内容 →
                </Link>
              )}
            </div>
          </article>
        ))}
      </div>
      {page?.has_more && (
        <button
          className="creator-secondary mx-auto mt-6"
          disabled={busy}
          onClick={() => void more()}
        >
          加载更多
        </button>
      )}
      <Dialog
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-xl">
          <DialogTitle>处理举报</DialogTitle>
          <DialogDescription>
            操作会保留管理员、理由及时间。恢复视频后仍需重新审核和发布，图文会恢复为草稿。
          </DialogDescription>
          {selected && (
            <Decision
              key={selected.report_id}
              report={selected}
              onDone={() => {
                setSelected(null);
                setReload((v) => v + 1);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
function Decision({ report, onDone }: { report: Report; onDone: () => void }) {
  const { apiBase } = useAuth();
  const [action, setAction] = useState('resolved'),
    [reason, setReason] = useState(''),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [history, setHistory] = useState<Page<ReportAction> | null>(null),
    [historyError, setHistoryError] = useState('');
  const pending = useRef(false);
  useEffect(() => {
    const c = new AbortController();
    void interactions
      .history(apiBase, report.report_id, '', c.signal)
      .then(({ data }) => {
        if (!c.signal.aborted) setHistory(data);
      })
      .catch((err) => {
        if (!c.signal.aborted) setHistoryError(interactionError(err));
      });
    return () => c.abort();
  }, [apiBase, report.report_id]);
  async function moreHistory() {
    try {
      const { data } = await interactions.history(
        apiBase,
        report.report_id,
        history?.next_cursor || '',
      );
      setHistory((p) => ({
        ...data,
        items: [
          ...(p?.items || []),
          ...data.items.filter(
            (a) => !p?.items.some((v) => v.action_id === a.action_id),
          ),
        ],
      }));
      setHistoryError('');
    } catch (err) {
      setHistoryError(interactionError(err));
    }
  }
  async function save() {
    if (pending.current || !reason.trim()) return;
    pending.current = true;
    setBusy(true);
    try {
      await interactions.resolve(
        apiBase,
        report.report_id,
        report.version,
        action,
        reason.trim(),
      );
      onDone();
    } catch (err) {
      setError(
        interactionError(err) +
          '；内容可能已更新，请关闭窗口并刷新列表后重试。',
      );
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <>
      <p className="max-h-36 overflow-y-auto whitespace-pre-wrap break-words rounded-lg bg-white/5 p-3">
        {report.preview || '内容不可用'}
      </p>
      <label className="text-sm">
        处理方式
        <select
          className="creator-input mt-2 w-full"
          value={action}
          onChange={(e) => setAction(e.target.value)}
        >
          {Object.entries(actions).map(([k, v]) => (
            <option
              disabled={
                (k === 'hidden' || k === 'restored') &&
                (!report.version || report.version === '0')
              }
              key={k}
              value={k}
            >
              {v}
            </option>
          ))}
        </select>
      </label>
      <label className="text-sm">
        处理理由
        <textarea
          className="creator-input mt-2 min-h-24 w-full"
          maxLength={1000}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
      </label>
      {error && (
        <p role="alert" className="text-red-300">
          {error}
        </p>
      )}
      <button
        className="creator-primary"
        disabled={busy || !reason.trim()}
        onClick={() => void save()}
      >
        {busy ? '保存中…' : '确认处理'}
      </button>
      <div className="mt-3 border-t border-white/10 pt-4">
        <h3 className="font-medium">处理记录</h3>
        {history?.items.map((h) => (
          <p
            key={h.action_id}
            className="mt-3 break-words text-xs leading-6 text-white/50"
          >
            {new Date(h.created_at).toLocaleString('zh-CN')} · 管理员{' '}
            {h.actor_id} · {actions[h.action] || h.action}
            <br />
            {h.reason}
          </p>
        ))}
        {history?.items.length === 0 && (
          <p className="mt-3 text-xs text-white/40">尚无处理记录</p>
        )}
        {historyError && <p className="text-red-300">{historyError}</p>}
        {(history?.has_more || historyError) && (
          <button
            className="mt-3 text-xs underline"
            onClick={() => void moreHistory()}
          >
            加载处理记录
          </button>
        )}
      </div>
    </>
  );
}
