# 9. Never encode meaning in colour alone; degrade to ASCII

- Status: accepted
- Date: 2026-09-01

## Context

kubeui shows health. Health is exactly the kind of information that tools tend
to encode as a red or green cell — which is unreadable for a colour-blind user,
invisible in a monochrome terminal, and lost entirely when the output is piped
into a file, a ticket or a CI log.

Terminals also vary in what they can draw: a serial console, an old conhost, or
a `TERM=dumb` environment cannot render `✓` and will show a replacement box.

## Decision

- Every state is rendered as **glyph + word**, coloured: `✓ healthy`,
  `⚠ degraded`, `✖ down`. Removing the colour must lose nothing.
- Colour support is detected and can be switched off: `NO_COLOR` (any value),
  `CLICOLOR=0` and `TERM=dumb` are honoured.
- The symbol set falls back to pure ASCII (`OK`, `!`, `X`, `>`) when the locale
  or terminal does not indicate UTF-8 support, or when `KUBEUI_ASCII=1` is set.
- Text attributes (bold, reverse) are a separate capability from colour: a
  monochrome terminal still gets structure, while a pipe or a file gets plain
  text with no escape sequences at all.
- Everything is reachable by keyboard, key bindings are configurable, and the
  layout stays usable down to 60x12.

## Consequences

- Status rendering goes through the theme's `Badge` helper rather than being
  styled ad hoc, which is a small constraint on every new view.
- Tests assert the plain-text rendering, so a regression that makes colour
  load-bearing fails CI rather than reaching a user who cannot see it.
