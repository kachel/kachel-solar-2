# kachel.solar — Project brief (v2)

A solar-powered Raspberry Pi server hosting a personal resume site, with live battery/power telemetry. Domain: **kachel.solar** (registered at Porkbun). Backup/about-me site: **kachel.work**.

## Status as of this handoff
- [x] Domain purchased (Porkbun)
- [x] Domain added to Cloudflare via **"Connect a domain"** (NOT "Transfer a domain" — Porkbun stays the registrar, Cloudflare manages DNS only)
- [x] Confirmed DNSSEC is off at Porkbun (checked "Registry DNSSEC" specifically, since Cloudflare's nameservers are third-party from Porkbun's perspective)
- [ ] Nameservers switched at Porkbun to Cloudflare's assigned pair — **in progress, waiting on propagation** (check Cloudflare dashboard for "Active" status, or whatsmydns.net)
- [ ] Cloudflare Tunnel (`cloudflared`) — blocked on Pi hardware, not started
- [ ] Static site build — **starting now**, this is the focus of this Claude Code session
- [ ] Hardware build — parts ordered/in transit, not assembled (see hardware section)

**Note on site hosting**: earlier versions of this brief planned to split hosting — static pages on Cloudflare Pages, only the live battery API on the Pi. That's been reversed: the whole site is self-hosted from the Pi, since splitting it undermined the "powered by the sun" premise. Cloudflare Tunnel is now purely the network path (DNS + secure tunnel), not a CDN/hosting layer. This means the static site work in this session should be built to actually run on the Pi's server later — still plain HTML/CSS/JS, no framework, but keep in mind it'll be served by the same tiny Go/Python process as the battery API, not deployed to a separate static host.

## Hardware (for context — not this session's task)
| Part | Notes |
|---|---|
| Raspberry Pi Zero (original, not Zero 2 W) | 32-bit (ARMv6) only |
| Voltaic P105 (5W, 6V) solar panel | PID 5369 |
| Adafruit bq24074 solar charger | PID 4755 |
| DC jack adapter cable (3.5/1.1mm → 5.5/2.1mm) | PID 2788 |
| Adafruit INA219 STEMMA QT breakout | PID 904 — voltage/current sensor |
| Adafruit PowerBoost 500 Charger | already owned |
| 3000mAh 3.7V LiPo/Li-ion battery | already owned |
| JST-PH 1-male-2-female splitter | to feed both charger and PowerBoost from one battery |

Full hardware wiring/power-chain detail available on request if this session needs it for the sensor API integration later — not required for the static site work.

### Software architecture
- **Everything is served from the Pi** — static pages and the live `/api/battery` endpoint alike. This was previously split (static site on Cloudflare Pages, only the API on the Pi) to protect against traffic spikes and home-bandwidth limits, but that split undermines the project's core premise: "hosted by me, powered by the sun" should be true of the whole site, not just one small JSON endpoint. Reversed this decision — full self-hosting through the tunnel is the intended architecture.
- **Server**: minimal Go or Python server on the Pi, no framework, serving both static files and the `/api/battery` route
- Cache the last INA219 reading server-side for 10-15s to avoid hammering the sensor on repeated requests
- Basic response caching headers (`Cache-Control`) on static assets still worth setting, even without a CDN in front — cheap, reduces repeat-visitor load
- **Accepted trade-off**: if the battery genuinely depletes overnight with no sun, the entire site goes down (not just the widget) until it recharges. This is treated as a feature, not a bug — a visitor landing on the 404/battery-empty page (with its kachel.work backup link) is itself proof the "solar powered" claim is real, not simulated.
- No CDN edge caching cushioning traffic spikes — accepted, since realistic traffic for a personal resume site (tens to low hundreds of visits on a busy day) doesn't approach the level where this matters
- **Networking** (later): Cloudflare Tunnel used purely as the network path (DNS + secure tunnel to the Pi) — NOT as a CDN/static-hosting layer. Tailscale (private, NOT Funnel) for the builder's own SSH access.
- Tiny footprint is a hard requirement throughout: no persistent database daemon, no unnecessary JS libraries, system fonts preferred over custom web fonts.

## Site structure

```
Shared layout (every page):
  - Persistent battery widget: icon + percentage + charging state
    (lightning bolt/sun icon when actively charging; distinct visual
    state for "stale data" if the Pi/API is unreachable — NOT the same
    as a 404, this is "data's old" not "page missing")
  - Nav links
  - "Hire me" widget: circular avatar icon (static for now, placeholder
    art) — on hover (desktop) or tap (mobile), a speech bubble pops out
    beside the circle reading "Hire me", with a small tail pointing back
    at the circle. Clicking the bubble opens the person's LinkedIn
    profile in a new tab. Needs a real illustrated avatar eventually
    (neutral state; a waving variant is an open option — not decided
    whether the avatar itself also swaps, or stays static under the bubble)
  - Favicon: solar/battery themed

├── Landing page (/)
│   Title: "Kachel — a solar-powered resume"
│   Para 1: "This is my solar powered resume website! Hosted by
│            [me] and powered by [the sun]."
│     - "me" links to https://kachel.work
│       aria-label="About me, hosted on kachel.work"
│       target="_blank" rel="noopener noreferrer" (leaving the site entirely)
│     - "the sun" links to /writeup
│       aria-label="Read the project write-up"
│   Para 2: "I'm looking for a job! Please hire me <3"
│   Button group, label "Pick a resume": Software dev / Sales / General
│     - consider a one-line blurb under each button to help visitors
│       self-select (e.g. "for technical/engineering roles")
│     - order buttons by priority role, not alphabetically
│
├── Resumes (/resume/dev, /resume/sales, /resume/general)
│   Same underlying job history across all three; framing/emphasis and
│   bullet wording differ per audience.
│   Layout: collapsible entries via native <details>/<summary> — zero
│   JS, accessible by default. Job title + company/dates in <summary>
│   (dates/location styled smaller, muted color — 13px, var(--text-secondary)
│   equivalent); bullets revealed on expand.
│   Most-relevant-to-that-audience entry open by default (not necessarily
│   most recent chronologically — e.g. sales variant may open the TAG
│   role by default even though NFI is more recent).
│   Needs: PDF download button, contact/CTA.
│
│   --- DRAFT CONTENT (sales-leaning framing, needs dev/general passes) ---
│
│   Job 1: UX Engineer | NFI (National Freight Industries), Chicago, IL
│   Apr 2019 – Sep 2025
│   - Ran workshops with product owners, engineers, and designers to
│     surface needs, align on solutions, and build organizational buy-in.
│   - Presented design proposals and compliance recommendations to
│     leadership, advocating for accessibility investments that resulted
│     in measurable WCAG compliance improvements.
│   - Co-owned a shared design system across a 10+ person UX team,
│     driving UI consistency across multiple enterprise applications.
│   - Designed and prototyped web components in Figma, then implemented
│     them 1:1 in production code, bridging design intent and engineering.
│
│   Job 2: Export Manager, Business Development Coordinator | TAG (Trade
│   Associates Group), Chicago, IL
│   May 2016 – Sep 2017
│   - Represented the company at trade shows as primary liaison,
│     soliciting new leads and converting prospects into accounts.
│   - Managed a portfolio of sales: targets, claims, disputes, purchase
│     orders, back orders.
│   - Built a new product catalog system from scratch; published a
│     twice-monthly overstock catalog driving incremental revenue.
│   - Partnered with marketing on email campaigns and catalog mailings
│     for outbound pipeline.
│   - Synthesized monthly sales data for board-level review, translating
│     raw numbers into actionable insights for executives.
│   - Managed orders and inventory allocation for flash sale accounts.
│
│   TODO: full dev-variant and general-variant bullet rewrites, education,
│   skills lists, additional roles if any exist beyond these two.
│
├── Write-up (/writeup)
│   Sections: Overview, Software, Hardware, Conclusion
│   (content not yet drafted — pulls from this brief once hardware/software
│   phases are further along)
│
├── Sensor info (/sensor)
│   Stats: energy over time, avg % charged, time spent charging
│   Plain-language labels for each stat; stale-data fallback state
│   (see shared layout note above) rather than a hard error when the
│   Pi's API is unreachable
│
└── 404 / battery-empty page
    Joke copy (battery-empty theme) + link to kachel.work as backup
```

## Accessibility notes
- Use `aria-label` on inline sentence links where visible text ("me", "the sun") lacks standalone context for screen readers navigating by link list.
- Native `<details>/<summary>` chosen specifically for keyboard/screen-reader support without custom JS.
- Icon-only buttons (hire-me avatar) need `aria-label` describing function.
- Mobile responsiveness required — resumes will often be opened from a phone via email/LinkedIn link.

## Metadata / sharing
- Open Graph tags (title, description, preview image) needed for clean LinkedIn/link-sharing previews.
- Favicon: solar/battery themed, not yet designed.

## Open decisions / TODO
- [ ] Finish dev and general resume variant copy (full pass, not just sales)
- [ ] Decide hover-vs-tap interaction model details for hire-me bubble (confirmed: LinkedIn is the destination; interaction pattern itself still open)
- [ ] Source real illustrated avatar art (replace icon placeholder)
- [ ] Decide "about me" content scope on kachel.work vs. what's duplicated here
- [ ] Write Open Graph meta description + preview image
- [ ] Confirm education section / additional roles for resume completeness
- [ ] Write-up page content (overview/software/hardware/conclusion) — draft once hardware phase progresses further
