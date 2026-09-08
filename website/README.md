# Correlux website

The public landing page and documentation for Correlux. Static HTML, CSS and browser JavaScript, with a small Node build that renders the repository's Markdown using Marked. No production JavaScript framework, analytics, CDN, remote fonts or third-party runtime requests.

## Local preview

Requires Node.js 20 or newer and the Go version specified in `../go.mod`.

```bash
cd website
npm ci
npm run build
npm run preview
```

Open **http://127.0.0.1:4173/correlux/**. The preview supports both `/` and the project Pages prefix so relative asset paths can be checked before publishing. Re-run the build after editing sources; generated files live in `dist/` and are ignored by Git.

## Validation

```bash
npm run check
npx playwright install chromium
npx playwright test
```

The static check validates local links, heading anchors, unique IDs and search destinations. Browser tests cover desktop and mobile layout, the demo, installation tabs, clipboard, FAQ, documentation search, keyboard navigation, no-JavaScript fallback and WCAG A/AA checks with axe. On a machine with Chrome already installed, set `CHROME_PATH` to its executable instead of downloading Chromium.

## Publish to GitHub Pages

1. Commit the website and `.github/workflows/pages.yml` to the repository.
2. In **Settings → Pages → Build and deployment → Source**, choose **GitHub Actions**.
3. Push to `main`, or run the **Website** workflow manually on `main`.

The workflow builds and checks pull requests. Only a successful build on `main` deploys. The expected project URL is **https://aronk11.github.io/correlux/**. No GitHub token or repository secret is needed beyond the workflow's scoped `GITHUB_TOKEN` permissions.

The build accepts `SITE_URL` for canonical URLs, the sitemap, social metadata and the 404 home link. It defaults to the public project URL. If you configure a custom domain, update the workflow's `SITE_URL` and the repository's Pages domain setting together.

Workflow setup follows [GitHub's custom Pages workflow documentation](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages).

## Content and design

- `index.html`: landing-page copy and the real Go terminal UI, compiled to WebAssembly with a sample-data adapter.
- `assets/style.css`: graphite surfaces and the product’s blue, green, amber and rose palette, CSS motion and reduced-motion support.
- `assets/main.js`: accessible tabs, clipboard feedback, viewport reveals and local documentation search.
- `getting-started.md`, `docs-index.md`: website-specific documentation.
- `scripts/build.mjs`: builds the homepage and 29 documentation pages from the source Markdown; rewrites repository links to local HTML and creates the section search index.
- `assets/logo.svg`: original geometric Correlux mark, designed for the terminal identity.
- `assets/social.png`: social-sharing card. Source layout is `social.html`; regenerate using `scripts/capture.mjs` with a running preview server.
- `assets/fonts/`: locally served DM Sans and IBM Plex Mono, with their SIL Open Font Licences. `scripts/fetch-fonts.py` is an optional maintainer utility to refresh them; builds do not access Google Fonts.

Keep claims in sync with the application implementation. The product specification includes future work. The browser demo uses the actual `internal/ui/app` model, renderer, keyboard handling, command registry and diagnosis engine. It never executes a `tea.Cmd`, reads kubeconfig, connects to Kubernetes, runs a shell or starts an external editor. Its adapter is excluded from native binaries by the `correlux_demo` build tag. It is deliberately user-controlled rather than auto-advancing. Continuous decorative motion is disabled when reduced motion is requested.

The user guide, architecture, contribution guide, security policy, code of conduct, product specification and the ADRs are rendered directly from the repository. Edit the source Markdown instead of generated HTML.


## Real terminal demo

`npm run build` also compiles `cmd/correlux-demo` to WebAssembly. The build copies
the pinned Bubble Tea module to a temporary directory and supplies the four
missing browser TTY hooks there; it changes neither the module cache nor the
repository's dependency versions. Browser input drives `Model.Update`, and
`Model.View` supplies ANSI output rendered safely as text and styled spans.

The approximately 11 MB compressed runtime loads only after **Start interactive
terminal** is pressed and runs in a Web Worker. Modern browsers must support
WebAssembly, Workers and DecompressionStream. Loading failures provide retry;
without JavaScript the sample and user-guide link remain available. No runtime
assets come from third-party CDNs. The actual source, not generated binaries,
is committed; Pages builds the matching runtime for each deployment.

The view picker exposes every implemented full-screen view and overlay. Keyboard
navigation, command search, filters, namespace/cluster switching and confirmation
forms use the original UI. Scale, restart and delete affect only local sample
data. Sample scale is bounded to 20 replicas. Logs are finite fixture output,
not a live cluster stream. Shells, external editors, filesystem access and native
clipboard commands are labelled as requiring the installed app.

Run `go test -tags=correlux_demo ./internal/ui/app` from the repository root for
fixture-adapter regressions. The website tests cover starting the compiled runtime,
navigation, the production challenge, local scale changes and reset.
