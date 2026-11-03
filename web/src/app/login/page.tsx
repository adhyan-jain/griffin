"use client";

import dynamic from "next/dynamic";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";
import { api, resolveAuthToken } from "@/lib/api";
import { supabaseConfigured } from "@/lib/supabase";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ErrorNote } from "@/components/page";

// Inlined at build time (NEXT_PUBLIC_*): `false` in production builds, so the dev-login UI below is
// eliminated from the bundle. Keep this local; an imported constant would not be tree-shaken.
const DEV_LOGIN_ENABLED = process.env.NEXT_PUBLIC_ENABLE_DEV_LOGIN === "true";

const Hero3D = dynamic(() => import("@/components/hero-3d"), { ssr: false });

const DEMO_NODES = ["RUNNING", "COMPLETED", "QUEUED", "COMPLETED", "AWAITING_APPROVAL", "RUNNING", "COMPLETED", "QUEUED", "COMPLETED", "RUNNING"].map(
  (status, i) => ({ id: `d${i}`, status }),
);

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    resolveAuthToken().then((t) => {
      if (t) router.replace("/");
    });
  }, [router]);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      router.replace("/");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Sign-in failed.");
      setBusy(false);
    }
  };

  return (
    <div className="relative flex min-h-dvh items-center justify-center overflow-hidden px-4 py-10">
      <Hero3D nodes={DEMO_NODES} className="pointer-events-none absolute inset-0 opacity-50 md:opacity-70" />
      <div className="absolute inset-0 bg-gradient-to-b from-background/20 via-background/60 to-background" aria-hidden />

      <main className="relative w-full max-w-sm rounded-2xl border bg-card/85 p-6 shadow-xl backdrop-blur-md">
        <h1 className="text-2xl font-semibold tracking-tight">Sign in to Griffin</h1>
        <p className="mt-1 text-sm text-muted-foreground">Your AI workforce is waiting for instructions.</p>

        <div className="mt-6 space-y-4">
          {supabaseConfigured && (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                void run(() => api.signIn(email, password));
              }}
            >
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input id="email" type="email" required autoComplete="username" className="h-11" value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  required
                  autoComplete="current-password"
                  className="h-11"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              <Button type="submit" className="h-11 w-full" disabled={busy}>
                {busy && <Loader2 className="animate-spin" />}
                Sign in
              </Button>
            </form>
          )}

          {DEV_LOGIN_ENABLED && (
            <Button type="button" variant="outline" className="h-11 w-full" disabled={busy} onClick={() => void run(() => api.login(email || undefined))}>
              Continue as dev user
            </Button>
          )}

          {!supabaseConfigured && !DEV_LOGIN_ENABLED && <ErrorNote message="Sign-in is not configured for this deployment." />}
          {error && <ErrorNote message={error} />}
        </div>
      </main>
    </div>
  );
}
