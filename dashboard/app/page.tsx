"use client";

import { useCallback, useEffect, useState } from "react";

import type { AppStat, Kind, Stats } from "@/lib/types";
import { api } from "@/lib/api";
import { dateKey, fmtDur, pct, prettyDate, shiftDate } from "@/lib/format";
import { Card, KIND_COLOR, ShareBar } from "@/components/Card";
import { FocusRing } from "@/components/FocusRing";
import { CategoryDonut, PALETTE } from "@/components/CategoryDonut";
import { WeekBars } from "@/components/WeekBars";
import { DeepWorkList } from "@/components/DeepWorkList";
import { Subjects } from "@/components/Subjects";
import { TopList } from "@/components/TopList";
import { Activities } from "@/components/Activities";
import { Timeline } from "@/components/Timeline";
import { Review } from "@/components/Review";
import { SettingsPanel } from "@/components/SettingsPanel";

function appKind(a: AppStat): Kind {
  if (a.productive * 2 > a.seconds) return "productive";
  if (a.distraction * 2 > a.seconds) return "distraction";
  return "neutral";
}

export default function Page() {
  const today = dateKey(new Date());
  const [date, setDate] = useState(today);
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);

  const refresh = useCallback(async () => {
    try {
      setStats(await api<Stats>(`/api/stats?date=${date}`));
      setUpdatedAt(new Date());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load");
    }
  }, [date]);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 30000);
    return () => clearInterval(t);
  }, [refresh]);

  const togglePause = async () => {
    if (!stats) return;
    await api("/api/tracking", { paused: !stats.paused });
    refresh();
  };

  const day = stats?.day;
  const isToday = date === today;
  const aiOn = !!stats?.ai.enabled && !!stats?.ai.configured;

  return (
    <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6">
      {/* ---------------------------------------------------- header */}
      <header className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-emerald-500/15 ring-1 ring-emerald-500/30">
            <span className="h-3.5 w-3.5 rounded-full border-[3px] border-emerald-400" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight">Arjan</h1>
            <p className="text-xs text-zinc-500">
              {updatedAt ? `Updated ${updatedAt.toLocaleTimeString("en-GB", { hour12: false })}` : "Loading…"}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-1 rounded-xl border border-zinc-800 bg-zinc-900/60 p-1">
          <button
            onClick={() => setDate(shiftDate(date, -1))}
            className="rounded-lg px-2.5 py-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
            aria-label="Previous day"
          >
            ‹
          </button>
          <span className="min-w-40 text-center text-sm font-medium">
            {isToday ? "Today" : prettyDate(date)}
          </span>
          <button
            onClick={() => setDate(shiftDate(date, 1))}
            disabled={isToday}
            className="rounded-lg px-2.5 py-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 disabled:opacity-30"
            aria-label="Next day"
          >
            ›
          </button>
          {!isToday && (
            <button
              onClick={() => setDate(today)}
              className="ml-1 rounded-lg bg-zinc-800 px-2.5 py-1 text-xs text-zinc-300 hover:bg-zinc-700"
            >
              Today
            </button>
          )}
        </div>

        <div className="flex items-center gap-2">
          {stats && (
            <button
              onClick={togglePause}
              title={stats.paused ? "Resume tracking" : "Pause tracking"}
              className={`flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium ${
                stats.paused
                  ? "border-zinc-700 bg-zinc-800 text-zinc-400"
                  : "border-emerald-800 bg-emerald-950/50 text-emerald-300"
              }`}
            >
              <span
                className={`h-2 w-2 rounded-full ${stats.paused ? "bg-zinc-500" : "animate-pulse bg-emerald-400"}`}
              />
              {stats.paused ? "Paused" : "Tracking"}
            </button>
          )}
          <button
            onClick={() => setShowSettings(true)}
            className="rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700"
          >
            ⚙ Settings
          </button>
        </div>
      </header>

      {error && (
        <div className="mb-6 rounded-xl border border-rose-900/60 bg-rose-950/40 p-4 text-sm text-rose-300">
          Can&apos;t reach Arjan: {error}. Is it running in the tray?
        </div>
      )}

      {stats && !aiOn && (
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-indigo-900/60 bg-indigo-950/30 p-4 text-sm text-indigo-200">
          <span>
            Turn on <strong>AI categorization</strong> to see which subjects and topics you studied (free with a
            Gemini key, or fully local with Ollama).
          </span>
          <button
            onClick={() => setShowSettings(true)}
            className="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
          >
            Set up
          </button>
        </div>
      )}
      {stats?.ai.enabled && stats.ai.lastError && (
        <div className="mb-6 rounded-xl border border-amber-900/60 bg-amber-950/30 p-4 text-sm text-amber-300">
          AI categorization error: {stats.ai.lastError}
        </div>
      )}

      {day && (
        <>
          {/* ------------------------------------- row 1: overview */}
          <div className="mb-6 grid gap-6 lg:grid-cols-3">
            <Card title="Focus" className="flex flex-col items-center justify-center">
              <FocusRing score={day.focusScore} />
              <p className="mt-3 text-center text-xs text-zinc-500">Productive ÷ total tracked time</p>
            </Card>

            <Card title={isToday ? "Today" : "This day"}>
              {day.total > 0 ? (
                <div className="space-y-4">
                  <div className="flex items-baseline gap-2">
                    <span className="text-3xl font-bold tabular-nums">{fmtDur(day.total)}</span>
                    <span className="text-sm text-zinc-500">tracked</span>
                  </div>
                  {(
                    [
                      ["Productive", day.productive, KIND_COLOR.productive],
                      ["Distraction", day.distraction, KIND_COLOR.distraction],
                      ["Other", day.other, KIND_COLOR.neutral],
                    ] as const
                  ).map(([label, secs, color]) => (
                    <div key={label}>
                      <div className="flex items-baseline justify-between text-sm">
                        <span className="text-zinc-300">{label}</span>
                        <span className="tabular-nums text-zinc-400">
                          {fmtDur(secs)} · {pct(secs, day.total)}%
                        </span>
                      </div>
                      <ShareBar pct={pct(secs, day.total)} color={color} />
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-zinc-500">Nothing tracked {isToday ? "yet today" : "on this day"}.</p>
              )}
            </Card>

            <Card title="Daily review">
              <Review day={day} isToday={isToday} aiOn={aiOn} />
            </Card>
          </div>

          {/* ---------------------------- row 2: learning + categories */}
          <div className="mb-6 grid gap-6 lg:grid-cols-3">
            <Card title="What you learned" className="lg:col-span-2">
              <Subjects subjects={day.bySubject} aiOn={aiOn} />
            </Card>
            <Card title="By category">
              <CategoryDonut data={day.byCategory} />
              <ul className="mt-2 space-y-1.5">
                {day.byCategory.map((c, i) => (
                  <li key={c.name} className="flex items-center justify-between text-sm">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: PALETTE[i % PALETTE.length] }} />
                      <span className="truncate text-zinc-300">{c.name}</span>
                    </span>
                    <span className="tabular-nums text-zinc-400">{fmtDur(c.seconds)}</span>
                  </li>
                ))}
              </ul>
            </Card>
          </div>

          {/* --------------------------------- row 3: apps + websites */}
          <div className="mb-6 grid gap-6 lg:grid-cols-2">
            <Card title="Apps">
              <TopList
                empty="No apps recorded."
                items={day.byApp.map((a) => ({
                  key: a.exe,
                  name: a.name,
                  seconds: a.seconds,
                  kind: appKind(a),
                }))}
              />
            </Card>
            <Card title="Websites">
              <TopList
                empty="No websites recorded (is the browser extension installed?)."
                items={day.bySite.map((s) => ({ key: s.site, name: s.site, seconds: s.seconds, kind: s.kind }))}
              />
            </Card>
          </div>

          {/* ------------------------------------- row 4: activities */}
          <Card title="What you did — lectures, pages & windows" className="mb-6">
            <Activities items={day.activities} />
          </Card>

          {/* ------------------------------ row 5: timeline + deep work */}
          <Card title="Timeline" className="mb-6">
            <Timeline blocks={day.timeline} firstTs={day.firstTs} lastTs={day.lastTs} />
            <div className="mt-5">
              <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-zinc-500">
                Deep work blocks (uninterrupted ≥ 45 min productive)
              </h3>
              <DeepWorkList blocks={day.deepWork} />
            </div>
          </Card>

          {/* ------------------------------------------ row 6: week */}
          <Card
            title="Last 7 days"
            action={<span className="text-xs text-zinc-500">Click a day to open it</span>}
          >
            <WeekBars
              days={(stats?.week ?? []).map((w) => ({ ...w, label: w.date === today ? "Today" : w.label }))}
              onSelect={setDate}
            />
            <div className="mt-2 flex items-center gap-4 text-xs text-zinc-500">
              {(["productive", "distraction", "neutral"] as Kind[]).map((k) => (
                <span key={k} className="flex items-center gap-1.5">
                  <span className="h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: KIND_COLOR[k] }} />
                  {k === "neutral" ? "Other" : k[0].toUpperCase() + k.slice(1)}
                </span>
              ))}
            </div>
          </Card>
        </>
      )}

      <footer className="mt-8 text-center text-xs text-zinc-600">
        Arjan · runs locally from your system tray · your data never leaves this computer
      </footer>

      {showSettings && <SettingsPanel onClose={() => setShowSettings(false)} onChanged={refresh} />}
    </main>
  );
}
