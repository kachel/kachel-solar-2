# kachel.solar — Design & content spec (v3)

This supersedes the layout/content sections of earlier briefs with a concrete, implementable design spec for the main page. Hardware/software architecture from prior briefs still applies and isn't repeated in full here — see `kachel-solar-project-brief-v2.md` for that context if needed.

## Status
- Domain: connected to Cloudflare, DNS propagating (see v2 brief)
- **This session's task**: build the main page per the spec below, mobile-first

## Design tokens

```css
:root {
  --bg-color-dark: #0f380f;
  --bg-color-medium: #9bbc0f;
  --bg-color-light: #F8FFDD;
  --text-color-light: #F8FFDD;
  --text-color-dark: #0f380f;
  --border-light: #F8FFDD;
  --border-dark: #0f380f;

  --padding: 24px;

  --ff: monospace;
  --fs-xxs: 10px;
  --fs-xs: 12px;
  --fs-sm: 14px;
  --fs-base: 18px;
  --fs-lg: 20px;
  --fs-xl: 24px;
}
```

**Global defaults**
- Text color: `--text-color-light` unless otherwise noted
- Page background: `dithering-effect.webp` (provided asset — a green-toned dithered leaf/nature photo, GameBoy-esque 2-tone-plus-dither palette matching the color tokens), tiled/covering full page, unless a section specifies its own background
- Font size: `--fs-base` (18px) unless otherwise noted
- Font family: monospace throughout

Reference mockups provided (3 mobile screens: landing/about+resume-picker+jobs, jobs expanded view, skills+projects) plus the dithering background asset itself — visual tone is retro/GameBoy-green, pixelated dither texture, monospace type, chip-style pill buttons.

## Layout: mobile-first, single page, 5 main sections + header + sticky footer

### Header
- Marquee: `✽ kachel's solar powered website ✽`
- Background: `--bg-color-dark`
- Width: 100%

### Section 1: About me
```html
<div class="about-me">
  <p>
    Hi, I'm Kachel. This is my solar powered resume! Hosted by
    <a href="https://kachel.work" aria-label="About me, hosted on kachel.work" target="_blank" rel="noopener noreferrer">me</a>
    and powered by the
    <a href="/writeup" aria-label="Read the project write-up">sun</a>.
  </p>
  <p>
    I'm just a gal who loves making websites, nature, and electronics tinkering.
  </p>
</div>
```
- Text centered
- Background: `--bg-color-dark` at 80% opacity
- `aria-label`s required on both inline links since visible text ("me", "sun") lacks standalone context for screen readers reading a link list

### Section 2: Resume header
- Simple header: `<h2>Resume</h2>` (background: `--bg-color-dark` at 80% opacity, matching other section headers in the spec)
- **The resume-focus picker (software/sales radio chips) is omitted for now.** Previously this section drove which job-drawer content displayed; that toggle logic is removed along with it.
- Jobs section (below) now just shows the sales copy directly and statically, since that's the only complete copy — no focus-based switching until/unless the picker is reintroduced
- Chip component styles (default/selected/hovered states) from the original spec are no longer in use, but kept documented below in case the picker returns later

### Section 3: Jobs (two `<details>` drawers, static sales content)

**Closed state**
- Background: `--text-color-dark` (i.e. dark green fill)
- Border: 2px solid `--border-light`
- Chevron indicator, **rotates between states**: points one direction closed, rotates (e.g. 180°) to the other direction when open — implement via CSS transform on the marker, transitioning with the `<details>` open/closed state (no JS required — can be done with the `::marker`/`::-webkit-details-marker` pseudo-element or a custom SVG chevron toggled via the `[open]` attribute selector)
- Border should only wrap the outside of the whole element (not each internal piece)

**Open state**
- Solid background on the details element itself; the *content* inside has its own background at 80% opacity
- No border-bottom in the open state (border wraps the outside only, per above)

**Sales copy — drawer 1**
```
UX Engineer                              (font-weight: semi-bold)
National Freight Industries (NFI)        (font-size: --fs-xs, 12px)
Chicago, IL | April 2019 – September 2025 (font-size: --fs-xxs, 10px)

- Ran workshops with product owners, engineers, and designers to surface
  needs, align on solutions, and build organizational buy-in.
- Presented design proposals and compliance recommendations to leadership,
  successfully advocating for accessibility investments that resulted in
  measurable WCAG compliance improvements.
- Co-owned a shared design system across a 10+ person UX team, driving UI
  consistency across multiple enterprise applications.
- Designed and prototyped web components in Figma, then implemented them
  1:1 in production code, bridging the gap between design intent and
  engineering execution.
```

**Sales copy — drawer 2**
```
Export Sales Manager                      (font-weight: semi-bold)
Trade Associates Group (TAG)               (font-size: --fs-xs, 12px)
Chicago, IL | May 2016 – September 2017    (font-size: --fs-xxs, 10px)

- Represented the company at trade shows as the primary liaison, actively
  soliciting new leads and converting prospects into accounts.
- Managed a portfolio of sales for flash and export accounts, claims,
  disputes, purchase orders, and back orders.
- Built an entirely new product catalog system from scratch; published a
  twice-monthly overstock catalog that drove incremental revenue from
  excess inventory.
- Partnered with marketing on email campaigns and catalog mailings to
  support outbound pipeline efforts.
- Synthesized monthly sales data for board-level review, translating raw
  numbers into actionable insights for executive stakeholders.
```

- Inner list is a `<ul>`; replace bullet markers with `✽` (e.g. `list-style: none` + `content: "✽"` in a `::before`, or `list-style-type` with a custom marker)
- List item font size: `--fs-sm` (14px)

**Download button** (now always visible, since there's no picker gating it)
- Label: "download" (with a download icon)
- Default: dark text, light background, dark border
- Hover/focus: reversed color combo (light text, dark background)

### Section 4: Skills
- `<h2>Skills</h2>` — dark background at 80% opacity
- Styled as a list of chip-style items (light background, dark text), matching the resume-picker chip look but not interactive/radio inputs — just styled `<li>` or `<span>` tags
- **Skill list (per provided image, this is the authoritative source over the earlier text draft which had a formatting gap)**:
  - Workshop Facilitation
  - User Testing
  - Prototyping
  - CRM & Sales Tools
  - Multilingual (Spanish, Mandarin, French)

### Section 5: Projects
- `<h2>Projects</h2>` — dark background at 80% opacity
- Same drawer-style visual treatment as the jobs section, but inner content is `<p>` tags, not a `<ul>`

**Project 1**
```
Solar powered resume                          (font-weight: semi-bold)
self-hosted website on a raspberry pi         (font-size: --fs-xs, 12px)

You are here! Learn more about this project from my <a href="/writeup">write-up</a>.
```

**Project 2**
```
Ham zone                                      (font-weight: semi-bold)
Chicagoland ham radio checker                 (font-size: --fs-xs, 12px)

A <a href="https://chicago-ham-zone.netlify.app/">tool</a> I built to check
the current time against a curated schedule of amateur radio nets and
tells you whether one is on the air.
```

### Sticky footer widget
- Width: 100%
- Background: `--bg-color-medium` at 80% opacity
- Two elements:
  1. **Contact button** (label: "contact") — **direct link to LinkedIn profile**: `https://www.linkedin.com/in/kachel-wittig-1a81a325/`, opens in a new tab (`target="_blank" rel="noopener noreferrer"`). No FAB/speed-dial needed — the earlier 3-option contact menu (LinkedIn, email, phone) was reconsidered: putting a personal email/phone number directly in page source is scrapeable and undesirable, so LinkedIn is the sole contact channel. The half-donut speed-dial reference is no longer applicable; this is now a plain button/link, same visual treatment (icon + label) as other buttons in the spec.
  2. **Battery percentage widget** — also functions as a button/link to the project write-up page

## Corrections applied from raw spec (for the record)
- Fixed `id="sasquatch"` → `id="sales"` (was mismatched with its `<label for="sales">`, breaking the label/input accessible association)
- Chevron: rotates between open/closed states (spec originally just said "pointing up" for closed with no stated open-state behavior — confirmed it should animate/rotate)
- Footer button label: "contact" (mockups were inconsistent between "contact" and "wave")
- Contact scope: settled on **LinkedIn only**, direct link — a 3-destination FAB (LinkedIn, email, phone) was considered and dropped over privacy concerns about exposing a personal email/phone number in page source
- Resume focus: **two** options only (software, sales) — general resume dropped from scope
- Skills list: using the image's list (Workshop Facilitation, User Testing, Prototyping, CRM & Sales Tools, Multilingual) over the text draft, which had a formatting gap (double bullet with a missing item between "Cross-functional Collaboration" and "CRM & Sales Tools")

## Open TODO
- [ ] Software-focused resume copy (jobs section content when "Software" chip is selected) — not yet written
- [ ] Confirm final skill list is complete/accurate (image-derived list used above)
- [ ] Desktop/tablet layout — spec above is mobile-first only; wider breakpoints not yet designed
