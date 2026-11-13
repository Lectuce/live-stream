import { AuthorSpace } from '@/components/author-space';

export default async function SpacePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AuthorSpace userId={id} />;
}
