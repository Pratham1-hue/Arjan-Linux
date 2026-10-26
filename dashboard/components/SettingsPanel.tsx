"use client";

import { useCallback, useEffect, useState } from "react";

import type { AIStatus, Settings } from "@/lib/types";
import { api } from "@/lib/api";

interface Preset {
  id: string;
  name: string;
  baseURL: string;
  model: string;
  keyURL?: string;
  note: string;
}

// Every provider here speaks the OpenAI chat-completions protocol.
// Model names change often: use "Load models" to pick a current one.
const PRESETS: Preset[] = [
  {
    id: "gemini",
    name: "Google Gemini",
    baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
    model: "gemini-3.8-flash",
    keyURL: "https://aistudio.google.com/apikey",
    note: "Free tier available — but only ~20 requests/day on flash models.",
  },
  {
    id: "anthropic",
    name: "Anthropic (Claude)",
    baseURL: "https://api.anthropic.com/v1",
    model: "claude-haiku-4-5",
    keyURL: "https://platform.claude.com/settings/keys",
    note: "Paid per use — Haiku is the cheapest and fast enough for this task.",
  },
  {
    id: "openai",
    name: "OpenAI",
    baseURL: "https://api.openai.com/v1",
    model: "gpt-5-mini",
    keyURL: "https://platform.openai.com/api-keys",
    note: "Paid. Use a small model; this task is easy.",
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    baseURL: "https://openrouter.ai/api/v1",
    model: "openrouter/auto",
    keyURL: "https://openrouter.ai/keys",
    note: "One key, many models (some free).",
  },
  {
    id: "groq",
    name: "Groq",
    baseURL: "https://api.groq.com/openai/v1",
    model: "openai/gpt-oss-20b",
    keyURL: "https://console.groq.com/keys",
    note: "Very fast; has a free tier.",
  },
  {
    id: "ollama",
    name: "Ollama (local, private)",
    baseURL: "http://localhost:11434/v1",
    model: "llama3.2",
    note: "Runs on your laptop — nothing leaves your machine. No key needed.",
  },
  {
    id: "custom",
    name: "Custom (OpenAI-compatible)",
    baseURL: "",
    model: "",
    note: "Any endpoint that implements /chat/completions.",
  },
];

interface Form {
  enabled: boolean;
  provider: string;
  baseURL: string;
  model: string;
  apiKey: string;
}

type Msg = { ok: boolean; text: string } | null;

const input =
  "w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 outline-none focus:border-indigo-500";
const btn =
  "rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-200 transition-colors hover:bg-zinc-700 disabled:opacity-50";

function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${on ? "bg-emerald-500" : "bg-zinc-700"}`}
    >
      <span
        className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all ${on ? "left-5" : "left-0.5"}`}
      />
    </button>
  );
}

export function SettingsPanel({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [status, setStatus] = useState<AIStatus | null>(null);
  const [form, setForm] = useState<Form>({ enabled: false, provider: "gemini", baseURL: "", model: "", apiKey: "" });
  const [models, setModels] = useState<string[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<Msg>(null);
  const [results, setResults] = useState<Record<string, string>[] | null>(null);

  const load = useCallback(async () => {
    const s = await api<Settings>("/api/settings");
    setSettings(s);
    setForm({ enabled: s.ai.enabled, provider: s.ai.provider || "custom", baseURL: s.ai.baseURL, model: s.ai.model, apiKey: "" });
    const st = await api<{ ai: AIStatus }>("/api/status");
    setStatus(st.ai);
  }, []);

  useEffect(() => {
    load().catch((e) => setMsg({ ok: false, text: String(e.message ?? e) }));
  }, [load]);

  // Refresh AI status while the panel is open.
  useEffect(() => {
    const t = setInterval(() => {
      api<{ ai: AIStatus }>("/api/status").then((s) => setStatus(s.ai)).catch(() => {});
    }, 3000);
    return () => clearInterval(t);
  }, []);

  const preset = PRESETS.find((p) => p.id === form.provider) ?? PRESETS[PRESETS.length - 1];

  const save = async (extra?: Partial<Form>) => {
    const f = { ...form, ...extra };
    const s = await api<Settings>("/api/settings", {
      ai: {
        enabled: f.enabled,
        provider: f.provider,
        baseURL: f.baseURL,
        model: f.model,
        apiKey: f.apiKey || undefined,
      },
    });
    setSettings(s);
    setForm((cur) => ({ ...cur, ...extra, apiKey: "" }));
    onChanged();
    return s;
  };

  const run = async (name: string, fn: () => Promise<void>) => {
    setBusy(name);
    setMsg(null);
    try {
      await fn();
    } catch (e) {
      setMsg({ ok: false, text: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(null);
    }
  };

  const choosePreset = (id: string) => {
    const p = PRESETS.find((x) => x.id === id);
    if (!p) return;
    setForm((f) => ({ ...f, provider: id, baseURL: p.baseURL || f.baseURL, model: p.model || f.model }));
    setModels([]);
    setResults(null);
  };

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/60" onClick={onClose}>
      <aside
        className="h-full w-full max-w-lg overflow-y-auto border-l border-zinc-800 bg-zinc-900 p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Settings</h2>
          <button onClick={onClose} className="text-2xl leading-none text-zinc-500 hover:text-zinc-200" aria-label="Close">
            ×
          </button>
        </div>

        {/* ---- AI ---- */}
        <section className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="font-medium">AI categorization</h3>
              <p className="mt-1 text-xs leading-relaxed text-zinc-500">
                Detects the subject and topic of what you study (e.g. “Machine Learning · Gradient Descent”) and
                categorizes anything <code>rules.json</code> doesn’t cover. Each distinct activity is sent once and cached.
              </p>
            </div>
            <Toggle
              label="Enable AI categorization"
              on={form.enabled}
              onChange={(v) => run("toggle", async () => { await save({ enabled: v }); })}
            />
          </div>

          <label className="block">
            <span className="mb-1 block text-xs text-zinc-400">Provider</span>
            <select className={input} value={preset.id} onChange={(e) => choosePreset(e.target.value)}>
              {PRESETS.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
            <span className="mt-1 block text-xs text-zinc-500">
              {preset.note}{" "}
              {preset.keyURL && (
                <a href={preset.keyURL} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">
                  Get a key ↗
                </a>
              )}
            </span>
          </label>

          <label className="block">
            <span className="mb-1 block text-xs text-zinc-400">Base URL</span>
            <input className={input} value={form.baseURL} onChange={(e) => setForm({ ...form, baseURL: e.target.value })} />
          </label>

          <label className="block">
            <span className="mb-1 block text-xs text-zinc-400">Model</span>
            <div className="flex gap-2">
              <input
                className={input}
                list="arjan-models"
                value={form.model}
                onChange={(e) => setForm({ ...form, model: e.target.value })}
              />
              <button
                className={`${btn} shrink-0`}
                disabled={busy !== null}
                onClick={() =>
                  run("models", async () => {
                    await save();
                    const r = await api<{ models: string[] }>("/api/ai/models");
                    setModels(r.models);
                    setMsg({ ok: true, text: `${r.models.length} models available — pick one from the Model box.` });
                  })
                }
              >
                {busy === "models" ? "Loading…" : "Load models"}
              </button>
            </div>
            <datalist id="arjan-models">
              {models.map((m) => (
                <option key={m} value={m} />
              ))}
            </datalist>
          </label>

          <label className="block">
            <span className="mb-1 block text-xs text-zinc-400">
              API key {settings?.ai.hasKey && <span className="text-emerald-400">— saved ({settings.ai.keyHint})</span>}
            </span>
            <input
              className={input}
              type="password"
              autoComplete="off"
              placeholder={settings?.ai.hasKey ? "Leave empty to keep the saved key" : preset.id === "ollama" ? "Not needed for Ollama" : "Paste your API key"}
              value={form.apiKey}
              onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
            />
            <span className="mt-1 block text-xs text-zinc-500">
              Stored locally with user-only permissions (DPAPI-encrypted on Windows) — never sent
              anywhere but your AI provider.
              {settings?.ai.hasKey && (
                <>
                  {" "}
                  <button
                    className="text-rose-400 hover:underline"
                    onClick={() => run("clearKey", async () => {
                      setSettings(await api<Settings>("/api/settings", { ai: { clearKey: true } }));
                      setMsg({ ok: true, text: "API key removed." });
                    })}
                  >
                    Remove key
                  </button>
                </>
              )}
            </span>
          </label>

          <div className="flex flex-wrap gap-2">
            <button
              className="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={busy !== null}
              onClick={() => run("save", async () => { await save(); setMsg({ ok: true, text: "Saved." }); })}
            >
              {busy === "save" ? "Saving…" : "Save"}
            </button>
            <button
              className={btn}
              disabled={busy !== null}
              onClick={() =>
                run("test", async () => {
                  await save();
                  const r = await api<{ results: Record<string, string>[]; latencyMs: number }>("/api/ai/test", {});
                  setResults(r.results);
                  setMsg({ ok: true, text: `Connection works (${(r.latencyMs / 1000).toFixed(1)}s).` });
                })
              }
            >
              {busy === "test" ? "Testing…" : "Test connection"}
            </button>
            <button
              className={btn}
              disabled={busy !== null}
              onClick={() => run("run", async () => {
                await save();
                setStatus(await api<AIStatus>("/api/ai/run", {}));
                setMsg({ ok: true, text: "Classifying now — the dashboard updates when it's done." });
              })}
            >
              Classify now
            </button>
            <button
              className={btn}
              disabled={busy !== null}
              onClick={() => {
                if (!confirm("Forget all AI labels and classify everything again?")) return;
                run("clear", async () => {
                  setStatus(await api<AIStatus>("/api/ai/clear", {}));
                  onChanged();
                  setMsg({ ok: true, text: "AI labels cleared; re-classifying." });
                });
              }}
            >
              Reset labels
            </button>
          </div>

          {msg && (
            <p className={`rounded-lg px-3 py-2 text-sm ${msg.ok ? "bg-emerald-950/50 text-emerald-300" : "bg-rose-950/50 text-rose-300"}`}>
              {msg.text}
            </p>
          )}

          {results && (
            <div className="rounded-lg border border-zinc-800 p-3 text-xs">
              <div className="mb-2 text-zinc-500">Test results</div>
              {results.map((r, i) => (
                <div key={i} className="mb-1.5">
                  <div className="truncate text-zinc-300">{r.title}</div>
                  <div className="text-zinc-500">
                    {r.kind} · {r.category}
                    {r.subject ? ` · ${r.subject}` : ""}
                    {r.topic ? ` · ${r.topic}` : ""}
                  </div>
                </div>
              ))}
            </div>
          )}

          {status && (
            <div className="rounded-lg bg-zinc-950/60 p-3 text-xs text-zinc-400">
              <div>
                Status:{" "}
                <span className={status.enabled ? "text-emerald-400" : "text-zinc-500"}>
                  {status.running ? "classifying…" : status.enabled ? (status.configured ? "on" : "on — needs a key") : "off"}
                </span>
              </div>
              <div>Labeled activities: {status.labeled} · waiting: {status.pending}</div>
              {status.lastRun > 0 && <div>Last run: {new Date(status.lastRun * 1000).toLocaleTimeString("en-GB")}</div>}
              {status.lastError && <div className="mt-1 text-rose-400">Last error: {status.lastError}</div>}
            </div>
          )}

          <p className="text-xs leading-relaxed text-zinc-500">
            <strong className="text-zinc-400">Privacy:</strong> only the app name, website domain and window/tab title are
            sent — never full URLs, incognito tabs, or anything rules.json already marks as a distraction. Choose Ollama to
            keep everything on this laptop.
          </p>
        </section>

        <hr className="my-6 border-zinc-800" />

        {/* ---- App ---- */}
        <section className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h3 className="font-medium">Start at login</h3>
              <p className="mt-1 text-xs text-zinc-500">Launch Arjan in the tray when you sign in.</p>
            </div>
            <Toggle
              label="Start at login"
              on={settings?.startWithWindows ?? false}
              onChange={(v) => run("auto", async () => {
                setSettings(await api<Settings>("/api/settings", { startWithWindows: v }));
              })}
            />
          </div>
          {settings && (
            <dl className="space-y-1 text-xs text-zinc-500">
              <div><dt className="inline text-zinc-400">Data folder: </dt><dd className="inline break-all font-mono">{settings.dataDir}</dd></div>
              <div><dt className="inline text-zinc-400">Rules file: </dt><dd className="inline break-all font-mono">{settings.rulesPath}</dd></div>
              <div><dt className="inline text-zinc-400">Dashboard: </dt><dd className="inline font-mono">{settings.dashboardURL}</dd></div>
            </dl>
          )}
        </section>
      </aside>
    </div>
  );
}
