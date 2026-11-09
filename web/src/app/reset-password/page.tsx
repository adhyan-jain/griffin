"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { getSupabase } from "@/lib/supabase";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ErrorNote } from "@/components/page";

const MIN_LENGTH = 10;

/** Why a recovery link failed, read from the address the email link opened (Supabase puts it after the #). */
function linkProblem(): string | null {
  if (typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  const code = params.get("error_code");
  if (code === "otp_expired") return "That link has expired or was already used. Ask for a new recovery email.";
  if (params.get("error")) return params.get("error_description")?.replace(/\+/g, " ") ?? "That link did not work. Ask for a new recovery email.";
  return null;
}

/**
 * Where the recovery email's link lands. The link signs you in for this one purpose; choose a new password here and
 * you are taken into the app. The password is sent once to the sign-in service and is not stored by Griffin.
 */
export default function ResetPasswordPage() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [waited, setWaited] = useState(false);
  const [problem] = useState(linkProblem);
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const sb = getSupabase();
    if (!sb) return;
    // The link's token is turned into a session by the client when it is created; this waits to hear that it was.
    const { data } = sb.auth.onAuthStateChange((event, session) => {
      if (session && (event === "PASSWORD_RECOVERY" || event === "SIGNED_IN" || event === "INITIAL_SESSION")) setReady(true);
    });
    void sb.auth.getSession().then(({ data: s }) => s.session && setReady(true));
    const t = window.setTimeout(() => setWaited(true), 4000);
    return () => {
      data.subscription.unsubscribe();
      window.clearTimeout(t);
    };
  }, []);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < MIN_LENGTH) return setError(`Use at least ${MIN_LENGTH} characters.`);
    if (password !== again) return setError("The two passwords are not the same.");
    const sb = getSupabase();
    if (!sb) return setError("Sign-in is not configured for this copy of the app.");
    setBusy(true);
    const { error: err } = await sb.auth.updateUser({ password });
    setBusy(false);
    if (err) return setError(err.message);
    toast.success("Password changed. You are signed in.");
    router.replace("/");
  };

  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-5 px-4 py-10">
      <div className="space-y-1">
        <h1 className="font-display text-3xl font-bold tracking-tight">Set a new password</h1>
        <p className="text-sm text-muted-foreground">Choose one you will remember, or let your password manager make one.</p>
      </div>

      {problem && <ErrorNote message={problem} />}

      {ready && !problem ? (
        <form onSubmit={save} className="space-y-4" noValidate>
          <div className="space-y-2">
            <Label htmlFor="new-password">New password</Label>
            <Input id="new-password" type="password" autoComplete="new-password" className="h-11" value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="new-password-again">Type it again</Label>
            <Input id="new-password-again" type="password" autoComplete="new-password" className="h-11" value={again} onChange={(e) => setAgain(e.target.value)} />
          </div>
          {error && <ErrorNote message={error} />}
          <Button type="submit" className="h-11 w-full" disabled={busy}>
            {busy && <Loader2 className="animate-spin motion-reduce:animate-none" />} Save password
          </Button>
        </form>
      ) : !problem ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
          {waited ? (
            "Open this page from the link in your recovery email. The link works once and expires."
          ) : (
            <>
              <Loader2 className="size-4 animate-spin motion-reduce:animate-none" /> Checking your recovery link
            </>
          )}
        </div>
      ) : null}
    </main>
  );
}
