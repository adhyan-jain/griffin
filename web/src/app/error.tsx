"use client";

import { useEffect } from "react";
import { TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState, PageContainer } from "@/components/page";

/** Shown when a page crashes. The rest of the app keeps working; "Try again" reloads just this page. */
export default function Error({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  useEffect(() => {
    console.error(error);
  }, [error]);
  return (
    <PageContainer>
      <EmptyState
        icon={<TriangleAlert />}
        title="Something went wrong on this page"
        description="Your work is safe. Try again, and if it keeps happening, check that the Griffin server is running."
        action={<Button onClick={reset}>Try again</Button>}
      />
    </PageContainer>
  );
}
