import type { OwnerVideo } from '@/lib/types';

export function canPublishVideo(video: OwnerVideo) {
  return video.publish_status === 'draft' && video.process_status === 'ready'
    && video.audit_status === 'approved' && !!video.play_url;
}

export function isPublicVideo(video: OwnerVideo) {
  return video.publish_status === 'published' && video.process_status === 'ready'
    && video.audit_status === 'approved';
}

export function videoStatus(video: OwnerVideo): string {
  if (video.audit_status === 'rejected') return '审核未通过';
  if (isPublicVideo(video)) return '已发布';
  if (video.publish_status === 'published') return '暂未公开';
  if (canPublishVideo(video)) return '可发布';
  if (video.process_status === 'failed') return '处理失败';
  if (!video.has_media && video.process_status === 'pending') return '草稿 · 未上传';
  if (video.process_status !== 'ready') return '处理中';
  if (video.audit_status === 'pending') return '等待审核';
  return '暂不可发布';
}
