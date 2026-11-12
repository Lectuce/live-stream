import { UploadPageContent } from '@/components/video-submission';

export default async function UploadPage({ searchParams }: {
  searchParams: Promise<{ video_id?: string | string[] }>;
}) {
  const params = await searchParams;
  const videoID = Array.isArray(params.video_id) ? params.video_id[0] : params.video_id;
  return <UploadPageContent videoID={videoID ?? ''} />;
}
