import { Skeleton } from "@/components/ui/skeleton";
import { PageContainer } from "@/components/page";

/** Shown while a page loads, so navigation never looks frozen. */
export default function Loading() {
  return (
    <PageContainer>
      <Skeleton className="mb-6 h-9 w-56" />
      <div className="space-y-3">
        <Skeleton className="h-28 w-full rounded-xl" />
        <Skeleton className="h-28 w-full rounded-xl" />
      </div>
    </PageContainer>
  );
}
