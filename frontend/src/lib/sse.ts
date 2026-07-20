// Minimal, dependency-free Server-Sent Events (SSE) parser.
//
// Consumes decoded text chunks (as produced by a streaming fetch body reader)
// and emits fully-formed SSE events. It is deliberately transport-agnostic and
// side-effect-free so it can be unit tested in isolation: feed it arbitrary
// chunk boundaries (including splits mid-line and multi-line `data:` fields)
// and it yields the same events a spec-compliant client would dispatch.
//
// Wire format handled (see logchef live-tail contract):
//   : ok                     -> comment {text: "ok"}
//   : hb                     -> comment {text: "hb"}  (heartbeat)
//   event: rows\ndata: [...] -> message {event: "rows", data: "[...]"}
//   event: notice\ndata:{..} -> message {event: "notice", data: "{..}"}
//   event: end\ndata: {..}   -> message {event: "end", data: "{..}"}

export interface SSEComment {
  type: "comment";
  text: string;
}

export interface SSEMessage {
  type: "message";
  event: string;
  data: string;
}

export type SSEEvent = SSEComment | SSEMessage;

export interface SSEParser {
  /** Feed a decoded text chunk; returns any events completed by this chunk. */
  push(chunk: string): SSEEvent[];
}

export function createSSEParser(): SSEParser {
  let buffer = "";
  let eventType = "";
  let dataLines: string[] = [];
  // Whether we have accumulated any field (event/data) for the current block.
  // A blank line only dispatches a message if at least one field was seen,
  // which prevents comment-only blocks (e.g. `: ok\n\n`) emitting empty rows.
  let hasFields = false;

  function reset() {
    eventType = "";
    dataLines = [];
    hasFields = false;
  }

  function dispatch(out: SSEEvent[]) {
    if (!hasFields) {
      reset();
      return;
    }
    out.push({
      type: "message",
      event: eventType || "message",
      data: dataLines.join("\n"),
    });
    reset();
  }

  function handleLine(line: string, out: SSEEvent[]) {
    if (line === "") {
      dispatch(out);
      return;
    }

    // Comment line: everything after the leading colon, with one optional
    // leading space stripped per the SSE spec.
    if (line.startsWith(":")) {
      out.push({ type: "comment", text: stripLeadingSpace(line.slice(1)) });
      return;
    }

    let field: string;
    let value: string;
    const colon = line.indexOf(":");
    if (colon === -1) {
      // Field name with no value.
      field = line;
      value = "";
    } else {
      field = line.slice(0, colon);
      value = stripLeadingSpace(line.slice(colon + 1));
    }

    hasFields = true;
    if (field === "event") {
      eventType = value;
    } else if (field === "data") {
      dataLines.push(value);
    }
    // Other fields (id, retry, unknown) are accepted but ignored.
  }

  return {
    push(chunk: string): SSEEvent[] {
      buffer += chunk;
      const out: SSEEvent[] = [];
      let nl: number;
      while ((nl = buffer.indexOf("\n")) !== -1) {
        let line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + 1);
        if (line.endsWith("\r")) {
          line = line.slice(0, -1);
        }
        handleLine(line, out);
      }
      return out;
    },
  };
}

function stripLeadingSpace(value: string): string {
  return value.startsWith(" ") ? value.slice(1) : value;
}
