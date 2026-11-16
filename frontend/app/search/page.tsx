import { DiscoverySearch } from '@/components/discovery-search';
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ q?: string | string[]; type?: string | string[] }>;
}) {
  const params = await searchParams;
  const q = Array.isArray(params.q) ? params.q[0] || '' : params.q || '';
  const type = Array.isArray(params.type) ? params.type[0] || 'video' : params.type || 'video';
  return (
    <DiscoverySearch
      term={q}
      kind={['video', 'user', 'live'].includes(type) ? type : 'video'}
    />
  );
}
