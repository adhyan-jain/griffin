import Link from "next/link";
import { Compass } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState, PageContainer } from "@/components/page";

export default function NotFound() {
  return (
    <PageContainer>
      <EmptyState
        icon={<Compass />}
        title="That page does not exist"
        description="The link may be old, or the item was deleted."
        action={<Button asChild><Link href="/">Back to Home</Link></Button>}
      />
    </PageContainer>
  );
}
