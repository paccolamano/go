# Contract: Wire Format

**Module**: `github.com/paccolamano/go/sse` | **Reference**: WHATWG HTML Living Standard,
"Server-sent events" | **Research**: [R-05](../research.md#r-05-event-validation-and-encoding),
[R-14](../research.md#r-14-decoder)

This file fixes the bytes the server side writes and how the decoder reads bytes. In the examples
below, `⏎` stands for a line feed (`\n`).

## Encoding (server side)

A field line is `name ":"`, followed by `" " value` when the value is not empty, then `⏎`.

| What | Bytes |
|------|-------|
| Event | `event: <Type>⏎` if `Type` ≠ `""`, then `id: <ID>⏎` if `ID` ≠ `""`, then one `data: <line>⏎` per line of `Data` (or `data:⏎` for an empty line), then `⏎` |
| Reconnection time | `retry: <milliseconds>⏎`, then `⏎` (sent once, with the first block) |
| Keep-alive | `: keep-alive⏎` |
| Position | `id: <ID>⏎⏎` |
| Empty position (reset with an empty history) | `id:⏎⏎` |

Rules:

- `Data` is split at CRLF, CR and LF. A trailing line break produces a final empty data line, so
  the client receives the trailing `⏎`.
- Values that start with spaces keep them: the field's separator space is written only before a
  non-empty value, and clients remove exactly one space.
- The first block of a stream combines the reconnection time with the position or the reset
  event in one write, for example `retry: 3000⏎id: K7Q2M9XW4TBA-41⏎⏎`.

### Examples

| `Event` | Bytes |
|---------|-------|
| `{Data: "hello"}` | `data: hello⏎⏎` |
| `{Type: "progress", ID: "7", Data: "50%"}` | `event: progress⏎id: 7⏎data: 50%⏎⏎` |
| `{Data: "a\r\nb\rc\n"}` | `data: a⏎data: b⏎data: c⏎data:⏎⏎` |
| `{Data: ""}` | `data:⏎⏎` |
| `{Data: "  x"}` | `data:   x⏎⏎` (separator space, then the two spaces of the value) |
| `{ID: "a\nb"}` | refused with `ErrInvalidEvent` |
| `{Data: "\xff"}` | refused with `ErrInvalidEvent` |

## Decoding (client side)

The decoder implements the standard's interpretation of an event stream:

1. **Bytes to text**: UTF-8, with each maximal invalid subpart replaced by U+FFFD. One byte order
   mark (`EF BB BF`) is skipped at the very start of the stream only.
2. **Lines** end at CRLF, LF or CR. A CR at the end of one read followed by an LF at the start of
   the next is one line break. The decoder never waits for the byte after a CR before processing
   the line.
3. **Each line**:
   - empty: dispatch (step 4);
   - starting with `:`: a comment, ignored;
   - containing `:`: the field name is the text before the first colon; the value is the rest,
     without one leading space, if present;
   - otherwise: the whole line is the field name, and the value is empty.
4. **Fields**:

   | Name | Effect |
   |------|--------|
   | `event` | type buffer = value |
   | `data` | data buffer += value + `⏎` |
   | `id` | if the value has no NUL: last event identifier buffer = value; otherwise ignored |
   | `retry` | if the value is only ASCII digits: reconnection time = value ms; otherwise ignored |
   | anything else | ignored |

5. **Dispatch** on an empty line:
   1. last event identifier = its buffer, which is not reset;
   2. if the data buffer is empty, clear the type buffer and yield nothing;
   3. otherwise remove one trailing `⏎` from the data, yield
      `Event{ID: last event identifier, Type: type buffer, Data: data}`, and clear the data and
      type buffers.
6. **End of stream**: a pending event without its empty line is discarded.
7. **Size**: a line, or the pending event's buffers together, longer than the maximum event size
   (1 MiB by default) end decoding with `ErrEventTooLarge`.

### Conformance vectors (from the standard)

These are every example event stream of the standard's "Server-sent events" section, checked on
2026-10-21: the two of the introduction and the four of the processing model.

| Stream | Events yielded |
|--------|----------------|
| `data: This is the first message.⏎⏎data: This is the second message, it⏎data: has two lines.⏎⏎data: This is the third message.⏎⏎` | `{Data: "This is the first message."}`, `{Data: "This is the second message, it\nhas two lines."}`, `{Data: "This is the third message."}` |
| `event: add⏎data: 73857293⏎⏎event: remove⏎data: 2153⏎⏎event: add⏎data: 113411⏎⏎` | `{Type: "add", Data: "73857293"}`, `{Type: "remove", Data: "2153"}`, `{Type: "add", Data: "113411"}` |
| `data: YHOO⏎data: +2⏎data: 10⏎⏎` | `{Data: "YHOO\n+2\n10"}` |
| `: test stream⏎⏎data: first event⏎id: 1⏎⏎data:second event⏎id⏎⏎data:  third event⏎⏎` | `{ID: "1", Data: "first event"}`, `{ID: "", Data: "second event"}`, `{ID: "", Data: " third event"}` |
| the same stream without its final `⏎` | the first two events only: the end of the stream does not dispatch the last one |
| `data⏎⏎data⏎data⏎⏎data:` | `{Data: ""}`, `{Data: "\n"}`; the last block is discarded |
| `data:test⏎⏎data: test⏎⏎` | two events with `Data: "test"` |

The implementation's test table also covers:

- CR-only and CRLF line endings;
- a byte order mark at the start and in the middle;
- `id` with NUL;
- `retry` with non-digits;
- a field without a value;
- unknown fields;
- the Unicode Table 3-8 substitutions;
- a CR split across two reads.

### Round trip (FR-011, SC-001)

For every valid `Event` `e`, encoding `e` and decoding the bytes yields one event `d` with:

- `d.Type == e.Type`;
- `d.Data` equal to `e.Data` with CRLF and CR replaced by LF;
- `d.ID == e.ID` when `e.ID` is not empty.
