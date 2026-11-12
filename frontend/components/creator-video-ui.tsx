import { Check, Clock3, FileVideo, ShieldAlert } from 'lucide-react';
import type { OwnerVideo } from '@/lib/types';
import { canPublishVideo, isPublicVideo, videoStatus } from '@/lib/video-status';

export function VideoStatusBadge({ video }: { video: OwnerVideo }) {
  const ready = canPublishVideo(video) || isPublicVideo(video);
  const rejected = video.audit_status === 'rejected' || video.process_status === 'failed';
  const pending = video.has_media && !ready && !rejected;
  const Icon = ready ? Check : rejected ? ShieldAlert : pending ? Clock3 : FileVideo;
  return <span className={`inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium ${ready ? 'bg-emerald-400/10 text-emerald-300' : rejected ? 'bg-red-400/10 text-red-300' : pending ? 'bg-amber-400/10 text-amber-200' : 'bg-white/6 text-white/55'}`}><Icon className="size-3" />{videoStatus(video)}</span>;
}

export function videoDuration(ms: number) {
  const seconds = Math.floor(ms / 1000);
  return `${Math.floor(seconds / 60).toString().padStart(2, '0')}:${(seconds % 60).toString().padStart(2, '0')}`;
}

export function videoDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date);
}
