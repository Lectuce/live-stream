import { VideoDiscovery } from '@/components/video-discovery';
import { videoCategories } from '@/lib/video-discovery';
import { notFound } from 'next/navigation';
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ sort?: string }>;
}) {
  const { id } = await params;
  const { sort } = await searchParams;
  if (!videoCategories.some(([key]) => key === id)) notFound();
  return (
    <VideoDiscovery
      category={id}
      mode={sort === 'hot' || sort === 'latest' ? sort : 'recommended'}
    />
  );
}
