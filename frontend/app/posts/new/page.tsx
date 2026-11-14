import { PostComposer } from '@/components/post-composer';

export default async function NewPostPage({ searchParams }: { searchParams: Promise<{ post_id?: string | string[] }> }) {
  const params = await searchParams;
  return <PostComposer id={(Array.isArray(params.post_id) ? params.post_id[0] : params.post_id) || ''} />;
}
