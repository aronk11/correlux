# Correlux website

The public site: a single static page, deployed to GitHub Pages from this
directory. It is deliberately separate from the Go project — nothing here
depends on the module, and nothing under `internal/`, `cmd/` or `docs/`
depends on this.

## Why no framework, and no build step

The whole site is one HTML page, one stylesheet and a few dozen lines of
vanilla JavaScript (a copy-to-clipboard button and a clock). A page this size
does not need React, a static-site generator or a CSS framework — pulling one
in would add a dependency chain, a `node_modules` tree and a build step to
maintain, in exchange for nothing this page needs. Hand-written HTML and CSS
also makes the Lighthouse and page-weight goals trivial to hit: there is no
unused framework CSS to ship and no client-side router doing the browser's
job.

Typography is a system font stack (`-apple-system`, `Segoe UI`, `Helvetica
Neue` … for text; `ui-monospace`, `SF Mono`, `Cascadia Mono` … for the
monospace treatment used throughout as a design element, not just for code).
That avoids a render-blocking web font request entirely, and the monospace
stack already reads as technical on every platform it resolves to.

There is no build step. Deployment (`.github/workflows/pages.yml`) uploads
this directory to GitHub Pages as-is.

## Structure

```
website/
  index.html            the entire page
  robots.txt
  sitemap.xml
  assets/
    css/style.css        one stylesheet, custom properties for the palette
    js/main.js            copy-to-clipboard + a clock, both optional
    img/
      favicon.svg          vector favicon (served to browsers that support it)
      favicon.ico           raster fallback, multi-resolution
      favicon-180.png       apple-touch-icon
      og-image.png           Open Graph / Twitter card image
```

## Previewing locally

Any static file server works, for example:

```bash
cd website
python3 -m http.server 8000
```

Then open `http://localhost:8000`.

## Design notes

- Palette: a reduced graphite/off-white pair for light and dark, plus four
  functional status colours (healthy, degraded, critical, unknown) that
  mirror the product's own rule — every status carries a glyph and a word,
  never colour alone.
- The fleet topology, the WHY block and the fleet-overview table reproduce
  real output shapes from the README and the product itself; anywhere a
  specific example is illustrative rather than a literal transcript, it is
  labelled as such.
- Motion is limited to a staggered reveal of the fleet rows and a single
  travelling signal dot; both are disabled under `prefers-reduced-motion:
  reduce`, and the page is fully usable with no motion at all.
