"use client";

import { useState } from "react";
import { toast } from "sonner";
import { api, type AlumniCard, type AlumniVerification } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type Campus = NonNullable<AlumniVerification["campus"]>["value"] | "none";
type AcmStatus = NonNullable<AlumniVerification["acm"]>["status"] | "none";

const CAMPUSES: Campus[] = ["none", "vellore", "chennai", "ap", "bhopal", "other"];
const ACM_STATUSES: AcmStatus[] = ["none", "confirmed", "likely", "not_confirmed"];

/** Owner evidence form: each claim needs a source URL and an excerpt. Only a non-LinkedIn source confirms. */
export function AlumniVerifyDialog({
  person,
  onClose,
  onSaved,
}: {
  person: AlumniCard | null;
  onClose: () => void;
  onSaved: (p: AlumniCard) => void;
}) {
  const [campus, setCampus] = useState<Campus>("none");
  const [campusSrc, setCampusSrc] = useState("");
  const [campusEx, setCampusEx] = useState("");
  const [year, setYear] = useState("");
  const [date, setDate] = useState("");
  const [gradSrc, setGradSrc] = useState("");
  const [gradEx, setGradEx] = useState("");
  const [acm, setAcm] = useState<AcmStatus>("none");
  const [role, setRole] = useState("");
  const [tenure, setTenure] = useState("");
  const [acmSrc, setAcmSrc] = useState("");
  const [acmEx, setAcmEx] = useState("");
  const [saving, setSaving] = useState(false);

  async function save() {
    if (!person) return;
    const body: AlumniVerification = {};
    if (campus !== "none") body.campus = { value: campus, source_url: campusSrc.trim(), excerpt: campusEx.trim() };
    if (year.trim()) {
      body.graduation = { year: Number(year), source_url: gradSrc.trim(), excerpt: gradEx.trim() };
      if (date) body.graduation.date = date;
    }
    if (acm !== "none") body.acm = { status: acm, role: role.trim(), tenure: tenure.trim(), source_url: acmSrc.trim(), excerpt: acmEx.trim() };
    setSaving(true);
    try {
      onSaved(await api.verifyAlumnus(person.id, body));
      toast.success("Evidence recorded");
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not record evidence");
    } finally {
      setSaving(false);
    }
  }

  const row = (label: string, value: string, set: (v: string) => void, type = "text") => (
    <Input aria-label={label} placeholder={label} type={type} value={value} onChange={(e) => set(e.target.value)} className="h-9 text-xs" />
  );

  return (
    <Dialog open={person !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Record evidence</DialogTitle>
          <DialogDescription>
            {person?.name}. Each claim needs a source URL and an excerpt. A LinkedIn source is self-reported and never confirms a campus or an ACM role.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 text-xs">
          <fieldset className="space-y-2">
            <legend className="mb-1 font-semibold">Campus</legend>
            <Select value={campus} onValueChange={(v) => setCampus(v as Campus)}>
              <SelectTrigger aria-label="Campus" className="h-9 w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {CAMPUSES.map((c) => <SelectItem key={c} value={c} className="capitalize">{c === "none" ? "No campus claim" : c}</SelectItem>)}
              </SelectContent>
            </Select>
            {campus !== "none" && (<>{row("Campus source URL", campusSrc, setCampusSrc)}{row("Campus excerpt", campusEx, setCampusEx)}</>)}
          </fieldset>
          <fieldset className="space-y-2">
            <legend className="mb-1 font-semibold">Graduation</legend>
            <div className="flex gap-2">{row("Graduation year", year, setYear, "number")}{row("Exact date (optional)", date, setDate, "date")}</div>
            {year.trim() !== "" && (<>{row("Graduation source URL", gradSrc, setGradSrc)}{row("Graduation excerpt", gradEx, setGradEx)}</>)}
          </fieldset>
          <fieldset className="space-y-2">
            <legend className="mb-1 font-semibold">ACM</legend>
            <Select value={acm} onValueChange={(v) => setAcm(v as AcmStatus)}>
              <SelectTrigger aria-label="ACM status" className="h-9 w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {ACM_STATUSES.map((s) => <SelectItem key={s} value={s}>{s === "none" ? "No ACM claim" : s.replace("_", " ")}</SelectItem>)}
              </SelectContent>
            </Select>
            {acm !== "none" && (
              <>
                <div className="flex gap-2">{row("ACM role", role, setRole)}{row("ACM tenure", tenure, setTenure)}</div>
                {row("ACM source URL", acmSrc, setAcmSrc)}
                {row("ACM excerpt", acmEx, setAcmEx)}
              </>
            )}
          </fieldset>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose} disabled={saving}>Cancel</Button>
            <Button onClick={save} disabled={saving || (campus === "none" && !year.trim() && acm === "none")}>{saving ? "Saving..." : "Save evidence"}</Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
