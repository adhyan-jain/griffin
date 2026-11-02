"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { api, getCurrentUser, clearAuthToken, setAuthToken, UserProfile } from "@/lib/api";
import { supabaseConfigured } from "@/lib/supabase";
import { Bot, Key, User, Lock, ShieldCheck, LogIn, LogOut, CheckCircle2, AlertCircle } from "lucide-react";

// Inlined at build time (NEXT_PUBLIC_*): `false` in production builds, so the dev-login code and
// strings below are removed from the bundle entirely, not merely hidden.
const DEV_LOGIN_ENABLED = process.env.NEXT_PUBLIC_ENABLE_DEV_LOGIN === "true";

export default function LoginPage() {
  const router = useRouter();
  const [currentUser, setCurrentUser] = useState<UserProfile | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [customToken, setCustomToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [statusMessage, setStatusMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    setCurrentUser(getCurrentUser());
  }, []);

  const handleSupabaseLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setStatusMessage(null);
    try {
      const user = await api.signIn(email.trim(), password);
      setPassword("");
      setCurrentUser(user);
      setStatusMessage({ type: "success", text: `Signed in as ${user.email}` });
      setTimeout(() => router.push("/"), 500);
    } catch (err) {
      setStatusMessage({ type: "error", text: err instanceof Error ? err.message : "Authentication failed." });
    } finally {
      setLoading(false);
    }
  };

  // Development builds only (NEXT_PUBLIC_ENABLE_DEV_LOGIN=true); unreachable in production builds.
  const handleDevLogin = async (devEmail: string, token?: string) => {
    if (!DEV_LOGIN_ENABLED) return;
    setLoading(true);
    setStatusMessage(null);
    try {
      if (token && token.trim()) {
        const userObj: UserProfile = { id: "custom-user", email: devEmail || "operator@griffin.cloud", role: "OPERATOR" };
        setAuthToken(token.trim(), userObj);
        setCurrentUser(userObj);
        setStatusMessage({ type: "success", text: "Custom session token saved (dev only)." });
        setTimeout(() => router.push("/"), 500);
        return;
      }
      const result = await api.login(devEmail);
      setCurrentUser(result.user);
      setStatusMessage({ type: "success", text: `Dev session as ${result.user.email}` });
      setTimeout(() => router.push("/"), 700);
    } catch (err) {
      setStatusMessage({ type: "error", text: err instanceof Error ? err.message : "Authentication failed." });
    } finally {
      setLoading(false);
    }
  };

  const handleLogout = () => {
    clearAuthToken();
    setCurrentUser(null);
    setStatusMessage({ type: "success", text: "Logged out successfully." });
  };

  return (
    <div className="max-w-xl mx-auto px-4 py-12">
      <div className="p-8 rounded-xl border border-slate-800 bg-slate-900/50 backdrop-blur shadow-2xl">
        <div className="flex items-center gap-3 mb-6">
          <div className="p-2.5 rounded-xl bg-blue-600/20 text-blue-400 border border-blue-500/30">
            <Bot className="w-8 h-8" />
          </div>
          <div>
            <h1 className="text-xl font-bold text-white tracking-tight">Griffin Access Control</h1>
            <p className="text-xs text-slate-400">Supabase Authenticated Workspace</p>
          </div>
        </div>

        {statusMessage && (
          <div
            className={`flex items-center gap-2 p-3 rounded-lg mb-6 text-xs border ${
              statusMessage.type === "success"
                ? "bg-emerald-950/40 text-emerald-300 border-emerald-800"
                : "bg-rose-950/40 text-rose-300 border-rose-800"
            }`}
          >
            {statusMessage.type === "success" ? (
              <CheckCircle2 className="w-4 h-4 shrink-0" />
            ) : (
              <AlertCircle className="w-4 h-4 shrink-0" />
            )}
            <span>{statusMessage.text}</span>
          </div>
        )}

        {currentUser ? (
          <div className="space-y-6">
            <div className="p-4 rounded-lg bg-slate-950 border border-slate-800 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-slate-400">Authenticated Session</span>
                <span className="inline-flex items-center gap-1 text-[11px] font-mono font-medium px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-400 border border-emerald-500/30">
                  <ShieldCheck className="w-3 h-3" /> ACTIVE
                </span>
              </div>
              <div className="space-y-1 text-xs">
                <div className="flex justify-between">
                  <span className="text-slate-400">User Email:</span>
                  <span className="font-mono text-white">{currentUser.email}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">User ID:</span>
                  <span className="font-mono text-slate-300 truncate max-w-[220px]" title={currentUser.id}>
                    {currentUser.id}
                  </span>
                </div>
              </div>
            </div>

            <div className="flex gap-3">
              <button
                onClick={() => router.push("/")}
                className="flex-1 py-2 px-4 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition shadow-sm"
              >
                Go to Dashboard
              </button>
              <button
                onClick={handleLogout}
                className="flex items-center justify-center gap-1.5 py-2 px-4 rounded-lg text-xs font-medium bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition"
              >
                <LogOut className="w-3.5 h-3.5" />
                <span>Log Out</span>
              </button>
            </div>
          </div>
        ) : (
          <div className="space-y-6">
            {supabaseConfigured ? (
              <form onSubmit={handleSupabaseLogin} className="space-y-4">
                <div>
                  <label htmlFor="email" className="block text-xs font-medium text-slate-300 mb-1.5">
                    Email
                  </label>
                  <div className="relative">
                    <User className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
                    <input
                      id="email"
                      type="email"
                      required
                      autoComplete="username"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      className="w-full pl-9 pr-3 py-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                    />
                  </div>
                </div>
                <div>
                  <label htmlFor="password" className="block text-xs font-medium text-slate-300 mb-1.5">
                    Password
                  </label>
                  <div className="relative">
                    <Lock className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
                    <input
                      id="password"
                      type="password"
                      required
                      autoComplete="current-password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      className="w-full pl-9 pr-3 py-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                    />
                  </div>
                </div>
                <button
                  type="submit"
                  disabled={loading}
                  className="w-full flex items-center justify-center gap-2 py-2 px-4 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white transition shadow-sm"
                >
                  <LogIn className="w-3.5 h-3.5" />
                  <span>{loading ? "Signing in..." : "Sign In"}</span>
                </button>
              </form>
            ) : (
              <p className="text-xs text-rose-300">
                Authentication is not configured. Set NEXT_PUBLIC_SUPABASE_URL and NEXT_PUBLIC_SUPABASE_ANON_KEY.
              </p>
            )}

            {DEV_LOGIN_ENABLED && (
              <div className="pt-4 border-t border-slate-800 space-y-3">
                <p className="text-[11px] text-amber-300">Development only — disabled in production builds.</p>
                <div className="relative">
                  <Key className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
                  <input
                    type="password"
                    value={customToken}
                    onChange={(e) => setCustomToken(e.target.value)}
                    placeholder="Paste a Supabase JWT (dev only)..."
                    className="w-full pl-9 pr-3 py-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                  />
                </div>
                <button
                  type="button"
                  onClick={() => handleDevLogin(email || "admin@griffin.internal", customToken)}
                  disabled={loading}
                  className="w-full py-1.5 px-3 rounded-lg text-xs text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 transition border border-slate-800"
                >
                  Use dev login
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
