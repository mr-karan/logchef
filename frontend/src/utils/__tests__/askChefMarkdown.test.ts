import { describe, expect, it } from "vitest";

import type { EvidenceId } from "@/api/askChef";
import { parseAnswer } from "../askChefMarkdown";

const known = new Set<EvidenceId>(["E1", "E2"]);

const text = (value: string, bold = false) => ({ kind: "text", text: value, bold });

describe("parseAnswer", () => {
  it("splits paragraphs on blank lines and keeps line breaks inside one", () => {
    expect(parseAnswer("First line\nsecond line\n\nNext paragraph", known)).toEqual({
      body: [
        { kind: "paragraph", inlines: [text("First line\nsecond line")] },
        { kind: "paragraph", inlines: [text("Next paragraph")] },
      ],
      notEstablished: null,
    });
  });

  it("renders bold, inline code and known citations as tokens", () => {
    const { body } = parseAnswer("**checkout-api failed** on `POST /orders` [E1][E2].", known);

    expect(body).toEqual([
      {
        kind: "paragraph",
        inlines: [
          text("checkout-api failed", true),
          text(" on "),
          { kind: "code", text: "POST /orders" },
          text(" "),
          { kind: "citation", id: "E1" },
          { kind: "citation", id: "E2" },
          text("."),
        ],
      },
    ]);
  });

  it("splits a grouped citation into one chip per known ID", () => {
    expect(parseAnswer("Both rose [E1, E2] and [E2,E9].", known).body).toEqual([
      {
        kind: "paragraph",
        inlines: [
          text("Both rose "),
          { kind: "citation", id: "E1" },
          { kind: "citation", id: "E2" },
          text(" and "),
          { kind: "citation", id: "E2" },
          text("[E9]."),
        ],
      },
    ]);
  });

  it("leaves an unknown evidence ID as literal text", () => {
    expect(parseAnswer("Seen at 06:04 [E9].", known).body).toEqual([{ kind: "paragraph", inlines: [text("Seen at 06:04 [E9].")] }]);
  });

  it("collects bullet lines into one list", () => {
    expect(parseAnswer("Causes:\n- payments-db refused [E1]\n* retries piled up", known).body).toEqual([
      { kind: "paragraph", inlines: [text("Causes:")] },
      {
        kind: "list",
        items: [[text("payments-db refused "), { kind: "citation", id: "E1" }], [text("retries piled up")]],
      },
    ]);
  });

  it("moves the Not established section out of the body", () => {
    const doc = parseAnswer("It fired [E1].\n\n**Not established**\n- Why payments-db refused connections", known);

    expect(doc.body).toEqual([{ kind: "paragraph", inlines: [text("It fired "), { kind: "citation", id: "E1" }, text(".")] }]);
    expect(doc.notEstablished).toEqual([
      { kind: "paragraph", inlines: [text("Not established", true)] },
      { kind: "list", items: [[text("Why payments-db refused connections")]] },
    ]);
  });

  it("keeps HTML, links and images as literal text", () => {
    const answer = '<img src=x onerror="alert(1)"> see [docs](https://example.com) ![x](https://example.com/a.png)';

    expect(parseAnswer(answer, known).body).toEqual([{ kind: "paragraph", inlines: [text(answer)] }]);
  });

  it("does not bold an unclosed marker while text is still streaming", () => {
    expect(parseAnswer("Errors rose **7.7", known).body).toEqual([{ kind: "paragraph", inlines: [text("Errors rose **7.7")] }]);
  });
});
