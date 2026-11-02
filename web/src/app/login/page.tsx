"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { api, getCurrentUser, setAuthToken, clearAuthToken, UserProfile } from "@/lib/api";
import { Bot, Key, User, ShieldCheck, LogIn, LogOut, CheckCircle2, AlertCircle } from "lucide-react";

export default function LoginPage() {
  const router = useRouter();
  const [currentUser, setCurrentUser] = useState<UserProfile | null>(null);
  const [email, setEmail] = useState("admin@griffin.internal");
  const [customToken, setCustomToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [statusMessage, setStatusMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    setCurrentUser(getCurrentUser());
  }, []);

  const handleLogin = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setLoading(true);
    setStatusMessage(null);

    try {
      if (customToken.trim()) {
        // If user manually pasted a Supabase JWT
        const userObj: UserProfile = {
          id: "custom-user",
          email: email || "operator@griffin.cloud",
          role: "OPERATOR",
        };
        setAuthToken(customToken.trim(), userObj);
        setCurrentUser(userObj);
        setStatusMessage({ type: "success", text: "Custom session token saved successfully." });
        setTimeout(() => router.push("/"), 500);
        return;
      }

      // Dev login endpoint
      const result = await api.login(email);
      setCurrentUser(result.user);
      setStatusMessage({ type: "success", text: `Authenticated as ${result.user.email} (Role: ${result.user.role})` });
      setTimeout(() => router.push("/"), 700);
    } catch (err: any) {
      setStatusMessage({ type: "error", text: err.message || "Authentication failed." });
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
            <p className="text-xs text-slate-400">Supabase JWT Authenticated Workspace</p>
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
                <div className="flex justify-between">
                  <span className="text-slate-400">Role:</span>
                  <span className="font-semibold text-blue-400">{currentUser.role}</span>
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
          <form onSubmit={handleLogin} className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-slate-300 mb-1.5">
                Operator Email
              </label>
              <div className="relative">
                <User className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="admin@griffin.internal"
                  className="w-full pl-9 pr-3 py-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                />
              </div>
              <p className="text-[11px] text-slate-400 mt-1">
                Enter your registered Supabase email or use default admin.
              </p>
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-300 mb-1.5">
                Optional: Custom Supabase Bearer JWT
              </label>
              <div className="relative">
                <Key className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
                <input
                  type="password"
                  value={customToken}
                  onChange={(e) => setCustomToken(e.target.value)}
                  placeholder="Paste raw Supabase JWT (optional)..."
                  className="w-full pl-9 pr-3 py-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                />
              </div>
            </div>

            <div className="pt-2 flex flex-col gap-2">
              <button
                type="submit"
                disabled={loading}
                className="w-full flex items-center justify-center gap-2 py-2 px-4 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white transition shadow-sm"
              >
                <LogIn className="w-3.5 h-3.5" />
                <span>{loading ? "Authenticating..." : "Sign In with Operator Credentials"}</span>
              </button>

              <button
                type="button"
                onClick={() => {
                  setEmail("admin@griffin.internal");
                  setCustomToken("");
                  handleLogin();
                }}
                disabled={loading}
                className="w-full py-1.5 px-3 rounded-lg text-xs text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 transition border border-transparent hover:border-slate-800"
              >
                Use Quick Dev Login (admin@griffin.internal)
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
