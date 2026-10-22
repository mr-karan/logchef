import { isEvidenceId, type EvidenceId } from "@/api/askChef";

// The answer renders through Vue text bindings, never v-html, so model output
// cannot inject markup. Links and images are not parsed and stay literal text.
export type InlineToken =
  | { kind: "text"; text: string; bold: boolean }
  | { kind: "code"; text: string }
  | { kind: "citation"; id: EvidenceId };

export type AnswerBlock =
  | { kind: "paragraph"; inlines: InlineToken[] }
  | { kind: "list"; items: InlineToken[][] };

interface AnswerDocument {
  body: AnswerBlock[];
  // The closing "Not established" section, or null when the answer has none.
  notEstablished: AnswerBlock[] | null;
}

interface EvidenceLookup {
  has(id: EvidenceId): boolean;
}

// A citation is one bracket of evidence IDs, such as [E1] or [E1, E3].
const INLINE = /`([^`\n]+)`|\*\*(.+?)\*\*|\[(E\d+(?:\s*,\s*E\d+)*)\]/g;
const BULLET = /^\s*[-*•]\s+(.*)$/;
const HEADING = /^\s*#{1,6}\s+(.*)$/;
const NOT_ESTABLISHED = /^\s*(?:#{1,6}\s+)?(?:\*\*)?Not established\b/i;

function pushText(out: InlineToken[], text: string, bold: boolean) {
  if (!text) return;
  const last = out[out.length - 1];
  if (last?.kind === "text" && last.bold === bold) last.text += text;
  else out.push({ kind: "text", text, bold });
}

function parseInline(text: string, evidence: EvidenceLookup, bold = false): InlineToken[] {
  const out: InlineToken[] = [];
  let cursor = 0;
  for (const match of text.matchAll(INLINE)) {
    pushText(out, text.slice(cursor, match.index), bold);
    const [whole, code, strong, ids] = match;
    if (code !== undefined) out.push({ kind: "code", text: code });
    else if (strong !== undefined) {
      for (const token of parseInline(strong, evidence, true)) {
        if (token.kind === "text") pushText(out, token.text, true);
        else out.push(token);
      }
    } else if (ids !== undefined) {
      for (const id of ids.split(",").map((part) => part.trim())) {
        if (isEvidenceId(id) && evidence.has(id)) out.push({ kind: "citation", id });
        else pushText(out, `[${id}]`, bold);
      }
    } else pushText(out, whole, bold);
    cursor = match.index + whole.length;
  }
  pushText(out, text.slice(cursor), bold);
  return out;
}

function parseBlocks(lines: string[], evidence: EvidenceLookup): AnswerBlock[] {
  const blocks: AnswerBlock[] = [];
  let paragraph: string[] = [];
  let list: InlineToken[][] | null = null;

  const flush = () => {
    if (paragraph.length) blocks.push({ kind: "paragraph", inlines: parseInline(paragraph.join("\n"), evidence) });
    if (list) blocks.push({ kind: "list", items: list });
    paragraph = [];
    list = null;
  };

  for (const line of lines) {
    const bullet = BULLET.exec(line);
    const heading = HEADING.exec(line);
    if (!line.trim()) flush();
    else if (bullet) {
      if (paragraph.length) flush();
      (list ??= []).push(parseInline(bullet[1], evidence));
    } else if (heading) {
      flush();
      blocks.push({ kind: "paragraph", inlines: parseInline(heading[1], evidence, true) });
    } else {
      if (list) flush();
      paragraph.push(line);
    }
  }
  flush();
  return blocks;
}

// Parses an assistant answer into blocks. Citations of unknown evidence stay literal text.
export function parseAnswer(markdown: string, evidence: EvidenceLookup): AnswerDocument {
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  const gapStart = lines.findIndex((line) => NOT_ESTABLISHED.test(line));
  if (gapStart === -1) return { body: parseBlocks(lines, evidence), notEstablished: null };
  return {
    body: parseBlocks(lines.slice(0, gapStart), evidence),
    notEstablished: parseBlocks(lines.slice(gapStart), evidence),
  };
}
