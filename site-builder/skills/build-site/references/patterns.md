# Premium motion patterns (field notes from the KEG «Conflict Control» build, 2026-07-19)

Reusable recipes for an impressive, modern, dark-premium site. Drop-in React/Next
components are vendored in `assets/patterns/` — copy into the project's
`src/components/` and adjust content/colors. All are Next 16 + Tailwind v4 +
framer-motion, reduced-motion aware.

## When to reach for these
Client says the first pass is "flat / boring / like the 90s". The lift comes from:
motion background, big overlapping editorial type, depth (glass/shadow/glow), and
tasteful micro-interactions — not just color.

## Design system that worked
- Dark **evergreen-charcoal + gold** (page `#0f140f`, surface `#161c16`, ink `#f1f3ea`,
  muted `#a7b09c`, sage `#9db489`, gold `#c79a46`/`#a6802f`). Grounded in UI/UX Pro Max
  "Luxury/Premium" + "Banking" palettes.
- Fonts: **Playfair Display** (headings) + **Inter** (body), `next/font/google` with
  `subsets: ["latin","cyrillic"]`.
- Base CSS helpers to add to `globals.css` (see the KEG globals for exact code):
  `.text-gold-grad` (gradient text for showpiece numbers), `.gold-rule` (thin gold
  top-border), `.hero-aurora` + `@keyframes aurora-*` (drifting blobs),
  `.liquid-glass-strong` (frosted glass w/ glowing edge, `@layer components`),
  `.link-underline` (gold draw-underline on hover).

## Components (in `assets/patterns/`)
- **HeroVideo** — full-bleed background `<video>` (muted/loop/inline, object-cover),
  pauses on reduced-motion. Pair with a dark legibility overlay + top/bottom fades.
- **HeroParticles** — canvas mote field (gold/sage drift) when there's no video; DPR-aware,
  tab-hidden pause, reduced-motion static. The `.hero-aurora` CSS blobs layer under it.
- **AnimatedHeadline** — headline that assembles word-by-word (fade+rise+de-blur stagger),
  gold gradient per word on the accent line.
- **CtaSection** — cinematic closing CTA (MotionSites "CTA+Footer" pattern): background
  video, frosted-glass primary + gold solid buttons, top/bottom fades.
- **LogicChain** — animated node chain for a process/formula (staggered nodes + drawing gold connectors).
- **Divider** — gold accent divider (line + centered diamond) instead of a plain border.
- **CTAButton** — pill with light-sheen sweep on hover, lift + active-press, gold focus ring.

## MotionSites.ai — the source of the video backgrounds
- Copy a template's **"Copy full prompt"** (it embeds the CloudFront/Mux video URL) and adapt
  the design to the brand — don't use it 1:1 (their jets/light themes ≠ our dark advisory).
- **Download the video locally** into `app/public/` and reference `/hero.mp4` — do NOT hotlink
  someone's CDN (fragile). Direct `.mp4` → `curl`. **Mux `.m3u8`** (HLS) → remux to mp4:
  `ffmpeg -y -i <m3u8> -c:v copy -an out.mp4` (video only; background is muted).
- Overlay every clip with a dark theme tint + legibility gradient so text stays readable and
  the footage matches the palette.

## Let the client choose — build a "lab" page
Screenshots via the browser tool are unreliable here. Instead scaffold a throwaway route
(`/video-lab`, `/logo-lab`, `robots: noindex`) that renders the candidate videos/logos in
context (as hero backgrounds, at real sizes) so the client views localhost and picks by number.
Delete the lab routes once finalized.

## Logo
Build a few distinct SVG emblem concepts + typographic wordmarks in a lab page; iterate.
Emblems use `currentColor` for strokes + a gold accent node so they inherit theme color.
(KEG chose interlocking rings — also read as a "CC" monogram — + a "CC / Conflict Control" wordmark.)

## Process notes
- Next 16: heed `AGENTS.md` ("read node_modules/next/dist/docs before coding"); `npm run build`
  must pass (static prerender) before calling it done.
- Commit per milestone; **tag versions** (`git tag v2 …`) so the client can name a snapshot to
  edit later. Big video assets: commit the chosen ones, gitignore the throwaway candidates.
- Delegating the first build to a sub-agent works, but supervise: a stalled agent left 3 pages
  unfinished once — verify by build + curl, take over if it goes quiet.
