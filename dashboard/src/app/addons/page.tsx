'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, Plus, Save, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
  createAddonStatusReport,
  deleteAddonStatusReport,
  fetchAddonStatusReports,
  replaceAddonStatusReport,
} from '@/lib/api';
import type {
  AddonChangelog,
  AddonFeature,
  AddonIssue,
  AddonSource,
  AddonStatusReport,
} from '@/lib/types';

const STATUS_OPTIONS: readonly string[] = ['LIVE', 'DOWN', 'MAINTENANCE'];

function emptyReport(): AddonStatusReport {
  return {
    addon: { id: '', name: '', status: 'LIVE', updatedAt: '' },
    sources: [],
    issues: [],
    changelog: [],
    features: [],
    changelogSourceUrl: '',
  };
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T;
}

function updateItem<T extends object>(arr: T[], i: number, patch: Partial<T>): T[] {
  return arr.map((x, idx) => (idx === i ? { ...x, ...patch } : x));
}
function removeItem<T>(arr: T[], i: number): T[] {
  return arr.filter((_, idx) => idx !== i);
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return <label className="mb-1 block text-xs font-medium text-muted-foreground">{children}</label>;
}

export default function AddonsPage() {
  const [reports, setReports] = useState<AddonStatusReport[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<AddonStatusReport | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetchAddonStatusReports();
      if (!res.success) throw new Error(res.error || 'Failed to load');
      setReports(res.reports || []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const startNew = () => {
    setEditing(emptyReport());
    setIsNew(true);
    setMsg(null);
  };
  const startEdit = (r: AddonStatusReport) => {
    setEditing(clone(r));
    setIsNew(false);
    setMsg(null);
  };
  const cancel = () => {
    setEditing(null);
    setMsg(null);
  };

  const save = async () => {
    if (!editing) return;
    if (!editing.addon.id.trim() || !editing.addon.name.trim()) {
      setMsg('Addon id and name are required.');
      return;
    }
    setSaving(true);
    setMsg(null);
    try {
      const res = isNew
        ? await createAddonStatusReport(editing)
        : await replaceAddonStatusReport(editing.addon.id, editing);
      if (!res.success) throw new Error(res.error || 'Save failed');
      setEditing(null);
      setIsNew(false);
      await load();
      setMsg('Saved.');
    } catch (e: unknown) {
      setMsg('Error: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: string) => {
    if (!window.confirm(`Delete the report for "${id}"? This removes it from the public site.`)) return;
    try {
      const res = await deleteAddonStatusReport(id);
      // 404 (already gone) is fine - the resource is absent either way.
      if (!res.success && !/not found/i.test(res.error || '')) {
        throw new Error(res.error || 'Delete failed');
      }
      await load();
      setMsg('Deleted.');
    } catch (e: unknown) {
      setMsg('Error: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  // ---- editing updaters ----
  const setAddon = (patch: Partial<AddonStatusReport['addon']>) =>
    setEditing((e) => (e ? { ...e, addon: { ...e.addon, ...patch } } : e));

  const addFeature = () =>
    setEditing((e) => (e ? { ...e, features: [...e.features, { title: '', body: '' }] } : e));
  const setFeature = (i: number, patch: Partial<AddonFeature>) =>
    setEditing((e) => (e ? { ...e, features: updateItem(e.features, i, patch) } : e));
  const removeFeature = (i: number) =>
    setEditing((e) => (e ? { ...e, features: removeItem(e.features, i) } : e));

  const addSource = () =>
    setEditing((e) => (e ? { ...e, sources: [...e.sources, { id: '', name: '', status: 'LIVE', detail: '' }] } : e));
  const setSource = (i: number, patch: Partial<AddonSource>) =>
    setEditing((e) => (e ? { ...e, sources: updateItem(e.sources, i, patch) } : e));
  const removeSource = (i: number) =>
    setEditing((e) => (e ? { ...e, sources: removeItem(e.sources, i) } : e));

  const addIssue = () =>
    setEditing((e) => (e ? { ...e, issues: [...e.issues, { id: '', title: '', status: 'investigating', summary: '', updatedAt: '' }] } : e));
  const setIssue = (i: number, patch: Partial<AddonIssue>) =>
    setEditing((e) => (e ? { ...e, issues: updateItem(e.issues, i, patch) } : e));
  const removeIssue = (i: number) =>
    setEditing((e) => (e ? { ...e, issues: removeItem(e.issues, i) } : e));

  const addChangelog = () =>
    setEditing((e) => (e ? { ...e, changelog: [...e.changelog, { version: '', date: '', highlights: [] }] } : e));
  const setChangelog = (i: number, patch: Partial<AddonChangelog>) =>
    setEditing((e) => (e ? { ...e, changelog: updateItem(e.changelog, i, patch) } : e));
  const removeChangelog = (i: number) =>
    setEditing((e) => (e ? { ...e, changelog: removeItem(e.changelog, i) } : e));
  const setHighlights = (i: number, text: string) =>
    setEditing((e) =>
      e
        ? {
            ...e,
            changelog: updateItem(e.changelog, i, {
              highlights: text.split('\n').map((s) => s.trim()).filter(Boolean),
            }),
          }
        : e,
    );
  const setChangelogSource = (url: string) =>
    setEditing((e) => (e ? { ...e, changelogSourceUrl: url } : e));

  return (
    <div className="mx-auto max-w-[1100px] p-4 sm:p-6 lg:p-8">
      <header className="mb-6 flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <Link href="/" className="text-muted-foreground transition hover:text-foreground">
            <ArrowLeft className="h-5 w-5" />
          </Link>
          <h1 className="text-2xl font-semibold text-foreground">Addon status &amp; content</h1>
        </div>
        <div className="flex items-center gap-2">
          <Link href="/">
            <Button variant="outline">Back to monitoring</Button>
          </Link>
          {!editing && (
            <Button variant="accent" onClick={startNew}>
              <Plus className="h-4 w-4" />
              New addon report
            </Button>
          )}
        </div>
      </header>

      {msg && <div className="mb-4 rounded-md border border-border bg-secondary/40 px-4 py-2 text-sm text-foreground">{msg}</div>}
      {error && !editing && (
        <div className="mb-4 rounded-md border border-destructive/40 bg-destructive/10 px-4 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      {editing ? (
        <ReportEditor
          report={editing}
          isNew={isNew}
          saving={saving}
          onChange={setEditing}
          onSetAddon={setAddon}
          onAddFeature={addFeature}
          onSetFeature={setFeature}
          onRemoveFeature={removeFeature}
          onAddSource={addSource}
          onSetSource={setSource}
          onRemoveSource={removeSource}
          onAddIssue={addIssue}
          onSetIssue={setIssue}
          onRemoveIssue={removeIssue}
          onAddChangelog={addChangelog}
          onSetChangelog={setChangelog}
          onRemoveChangelog={removeChangelog}
          onSetHighlights={setHighlights}
          onSetChangelogSource={setChangelogSource}
          onSave={save}
          onCancel={cancel}
        />
      ) : (
        <>
          {loading ? (
            <p className="text-sm text-muted-foreground">Loading...</p>
          ) : reports.length === 0 ? (
            <Card>
              <CardContent className="py-10 text-center text-sm text-muted-foreground">
                No addon reports yet. Click &quot;New addon report&quot; to create one.
              </CardContent>
            </Card>
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {reports.map((r) => (
                <Card key={r.addon.id}>
                  <CardHeader>
                    <CardTitle className="flex items-center justify-between gap-3">
                      <span className="text-foreground">{r.addon.name || r.addon.id}</span>
                      <span className="rounded-full border border-border bg-secondary px-2.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                        {r.addon.status}
                      </span>
                    </CardTitle>
                    <p className="text-xs text-muted-foreground">
                      id: <code className="text-foreground">{r.addon.id}</code> - updated {r.addon.updatedAt}
                    </p>
                  </CardHeader>
                  <CardContent>
                    <div className="flex gap-2">
                      <Button size="sm" variant="outline" onClick={() => startEdit(r)}>
                        Edit
                      </Button>
                      <Button size="sm" variant="destructive" onClick={() => void remove(r.addon.id)}>
                        <Trash2 className="h-4 w-4" />
                        Delete
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

interface EditorProps {
  report: AddonStatusReport;
  isNew: boolean;
  saving: boolean;
  onChange: (r: AddonStatusReport | null) => void;
  onSetAddon: (patch: Partial<AddonStatusReport['addon']>) => void;
  onAddFeature: () => void;
  onSetFeature: (i: number, patch: Partial<AddonFeature>) => void;
  onRemoveFeature: (i: number) => void;
  onAddSource: () => void;
  onSetSource: (i: number, patch: Partial<AddonSource>) => void;
  onRemoveSource: (i: number) => void;
  onAddIssue: () => void;
  onSetIssue: (i: number, patch: Partial<AddonIssue>) => void;
  onRemoveIssue: (i: number) => void;
  onAddChangelog: () => void;
  onSetChangelog: (i: number, patch: Partial<AddonChangelog>) => void;
  onRemoveChangelog: (i: number) => void;
  onSetHighlights: (i: number, text: string) => void;
  onSetChangelogSource: (url: string) => void;
  onSave: () => void;
  onCancel: () => void;
}

function StatusSelect({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <Select value={value || 'LIVE'} onValueChange={onChange}>
      <SelectTrigger className="w-[150px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {STATUS_OPTIONS.map((s) => (
          <SelectItem key={s} value={s}>
            {s}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function RowCard({
  index,
  title,
  onRemove,
  children,
}: {
  index: number;
  title: string;
  onRemove: (i: number) => void;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-md border border-border bg-background/40 p-3">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {title} #{index + 1}
        </span>
        <Button size="sm" variant="ghost" className="h-7 px-2 text-muted-foreground" onClick={() => onRemove(index)}>
          <Trash2 className="h-3.5 w-3.5" />
          Remove
        </Button>
      </div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">{children}</div>
    </div>
  );
}

function ReportEditor(p: EditorProps) {
  const { report: r } = p;
  return (
    <div className="space-y-6">
      {/* Addon meta */}
      <Card>
        <CardHeader>
          <CardTitle>Addon</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <FieldLabel>Id (slug)</FieldLabel>
              <Input
                value={r.addon.id}
                disabled={!p.isNew}
                onChange={(e) => p.onSetAddon({ id: e.target.value })}
                placeholder="tpb-4k-porn"
              />
            </div>
            <div>
              <FieldLabel>Name</FieldLabel>
              <Input value={r.addon.name} onChange={(e) => p.onSetAddon({ name: e.target.value })} />
            </div>
            <div>
              <FieldLabel>Status</FieldLabel>
              <StatusSelect value={r.addon.status} onChange={(v) => p.onSetAddon({ status: v })} />
            </div>
            <div>
              <FieldLabel>Updated at (YYYY-MM-DD)</FieldLabel>
              <Input
                value={r.addon.updatedAt}
                onChange={(e) => p.onSetAddon({ updatedAt: e.target.value })}
                placeholder="2026-06-25"
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Features */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            Features
            <Button size="sm" variant="outline" onClick={p.onAddFeature}>
              <Plus className="h-4 w-4" />
              Add feature
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {r.features.length === 0 && <p className="text-sm text-muted-foreground">No features.</p>}
          {r.features.map((f, i) => (
            <RowCard key={i} index={i} title="Feature" onRemove={p.onRemoveFeature}>
              <div className="sm:col-span-2">
                <FieldLabel>Title</FieldLabel>
                <Input value={f.title} onChange={(e) => p.onSetFeature(i, { title: e.target.value })} />
              </div>
              <div className="sm:col-span-2">
                <FieldLabel>Body</FieldLabel>
                <Textarea rows={2} value={f.body} onChange={(e) => p.onSetFeature(i, { body: e.target.value })} />
              </div>
            </RowCard>
          ))}
        </CardContent>
      </Card>

      {/* Sources */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            Sources (status section)
            <Button size="sm" variant="outline" onClick={p.onAddSource}>
              <Plus className="h-4 w-4" />
              Add source
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {r.sources.length === 0 && <p className="text-sm text-muted-foreground">No sources.</p>}
          {r.sources.map((s, i) => (
            <RowCard key={i} index={i} title="Source" onRemove={p.onRemoveSource}>
              <div>
                <FieldLabel>Id</FieldLabel>
                <Input value={s.id} onChange={(e) => p.onSetSource(i, { id: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Name</FieldLabel>
                <Input value={s.name} onChange={(e) => p.onSetSource(i, { name: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Note (optional)</FieldLabel>
                <Input value={s.note || ''} onChange={(e) => p.onSetSource(i, { note: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Status</FieldLabel>
                <StatusSelect value={s.status} onChange={(v) => p.onSetSource(i, { status: v })} />
              </div>
              <div className="sm:col-span-2">
                <FieldLabel>Detail</FieldLabel>
                <Input value={s.detail} onChange={(e) => p.onSetSource(i, { detail: e.target.value })} />
              </div>
            </RowCard>
          ))}
        </CardContent>
      </Card>

      {/* Issues */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            Issues
            <Button size="sm" variant="outline" onClick={p.onAddIssue}>
              <Plus className="h-4 w-4" />
              Add issue
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {r.issues.length === 0 && <p className="text-sm text-muted-foreground">No issues.</p>}
          {r.issues.map((it, i) => (
            <RowCard key={i} index={i} title="Issue" onRemove={p.onRemoveIssue}>
              <div>
                <FieldLabel>Id</FieldLabel>
                <Input value={it.id} onChange={(e) => p.onSetIssue(i, { id: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Title</FieldLabel>
                <Input value={it.title} onChange={(e) => p.onSetIssue(i, { title: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Status</FieldLabel>
                <Input
                  value={it.status}
                  onChange={(e) => p.onSetIssue(i, { status: e.target.value })}
                  placeholder="investigating"
                />
              </div>
              <div>
                <FieldLabel>Updated at</FieldLabel>
                <Input value={it.updatedAt} onChange={(e) => p.onSetIssue(i, { updatedAt: e.target.value })} />
              </div>
              <div className="sm:col-span-2">
                <FieldLabel>Summary</FieldLabel>
                <Textarea rows={2} value={it.summary} onChange={(e) => p.onSetIssue(i, { summary: e.target.value })} />
              </div>
            </RowCard>
          ))}
        </CardContent>
      </Card>

      {/* Changelog */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            Changelog
            <Button size="sm" variant="outline" onClick={p.onAddChangelog}>
              <Plus className="h-4 w-4" />
              Add entry
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div>
            <FieldLabel>Changelog source URL (public GitHub CHANGELOG.md raw URL)</FieldLabel>
            <Input
              value={r.changelogSourceUrl || ''}
              onChange={(e) => p.onSetChangelogSource(e.target.value)}
              placeholder="https://raw.githubusercontent.com/<owner>/<repo>/main/CHANGELOG.md"
            />
            <p className="mt-1.5 text-xs text-muted-foreground">
              When set, the backend fetches and parses this file on the public GET
              (cached 5 min) and the manual entries below are only a fallback if the
              fetch fails. Leave empty to use the manual entries.
            </p>
          </div>
          {r.changelog.length === 0 && <p className="text-sm text-muted-foreground">No changelog entries.</p>}
          {r.changelog.map((c, i) => (
            <RowCard key={i} index={i} title="Entry" onRemove={p.onRemoveChangelog}>
              <div>
                <FieldLabel>Version</FieldLabel>
                <Input value={c.version} onChange={(e) => p.onSetChangelog(i, { version: e.target.value })} />
              </div>
              <div>
                <FieldLabel>Date</FieldLabel>
                <Input value={c.date} onChange={(e) => p.onSetChangelog(i, { date: e.target.value })} />
              </div>
              <div className="sm:col-span-2">
                <FieldLabel>Highlights (one per line)</FieldLabel>
                <Textarea
                  rows={3}
                  value={c.highlights.join('\n')}
                  onChange={(e) => p.onSetHighlights(i, e.target.value)}
                />
              </div>
            </RowCard>
          ))}
        </CardContent>
      </Card>

      <div className="flex items-center gap-3">
        <Button variant="accent" onClick={p.onSave} disabled={p.saving}>
          <Save className="h-4 w-4" />
          {p.saving ? 'Saving...' : 'Save'}
        </Button>
        <Button variant="outline" onClick={p.onCancel} disabled={p.saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}