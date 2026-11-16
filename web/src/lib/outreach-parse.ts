/** Turns pasted lines into people. Accepts "Name <handle>", "Name, handle" or just a handle. */
export function parsePeople(text: string): { name: string; handle: string }[] {
  const seen = new Set<string>();
  const out: { name: string; handle: string }[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    let name = "";
    let handle = line;
    const angle = line.match(/^(.*?)<([^>]+)>\s*$/);
    const comma = line.match(/^([^,]+),\s*(\S+)\s*$/);
    if (angle) [name, handle] = [angle[1].trim(), angle[2].trim()];
    else if (comma) [name, handle] = [comma[1].trim(), comma[2].trim()];
    const key = handle.toLowerCase();
    if (!handle || seen.has(key)) continue;
    seen.add(key);
    out.push({ name, handle });
  }
  return out;
}
