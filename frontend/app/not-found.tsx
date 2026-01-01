import Link from "next/link";
import { Button } from "@/components/ui/button";
import { FileX, Home } from "lucide-react";

export default function NotFound() {
  return (
    <div className="flex flex-col items-center justify-center min-h-[calc(100vh-4rem)] p-4 text-center">
      <div className="flex h-24 w-24 items-center justify-center rounded-full bg-muted/50 mb-8 animate-in zoom-in-50 duration-300">
        <FileX className="h-12 w-12 text-muted-foreground" aria-hidden="true" />
      </div>
      
      <h1 className="text-4xl font-bold tracking-tight mb-2">Page not found</h1>
      <p className="text-muted-foreground max-w-[500px] mb-8 text-lg">
        Sorry, we couldn&apos;t find the page you&apos;re looking for. It might have been moved or doesn&apos;t exist.
      </p>
      
      <div className="flex gap-4">
        <Button asChild size="lg">
          <Link href="/dashboard">
            <Home className="mr-2 h-4 w-4" />
            Back to Dashboard
          </Link>
        </Button>
        <Button variant="outline" size="lg" asChild>
          <Link href="/inbox">
            Go to Inbox
          </Link>
        </Button>
      </div>
    </div>
  );
}
