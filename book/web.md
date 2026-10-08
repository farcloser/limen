# Project websites

A project site is a few static pages that say what the project is, how to get it, and
what it does, for someone who already knows the domain. No build step, no framework, no
marketing: the page is the file on disk, and every claim on it is one the project's own
tooling can check. The rules here come from the sites we run; each names the trap it
prevents and where it is enforced today. Until a second site repository exists, limen
carries no site lane: the book states each rule, and the site keeps its own recipe.

## Web

- **A site loads nothing from elsewhere.** No web font, no script from a CDN, no hotlinked
  image, no `preconnect`. Every load from another host is a request that host receives
  from every reader on every visit, and an unpinned dependency on that host's continued
  behaviour and content: the same objection the book's pinning rule makes about a binary
  fetched at build time. System typefaces, an inline mark, and the project's own files.
  Two mechanisms enforce it. A test over the markup (`just test`) fails on any
  `<link>` with a `stylesheet`, `preconnect`, `preload`, `modulepreload`, `icon` or
  `manifest` rel whose `href` is absolute, any `<script>`, `<img>`, `<iframe>`, `<source>`,
  `<video>`, `<audio>`, `<object>` or `<embed>` whose `src` is absolute, and any `@import`
  or `url()` in a stylesheet that names a host; a `canonical` or `og:` link is a
  declaration, not a load, and is not matched. Behind the test, the pages are served with
  a same-origin `Content-Security-Policy` (`default-src 'none'` and `'self'` per
  directive), so a load that slips past the markup is refused by the browser. The trap is
  the one inline script a page needs, the three lines that apply a stored theme before
  first paint: it is allowed by the hash of its exact bytes, and the hash in the policy
  must follow every edit to the snippet, or the theme flashes on load and nothing in CI
  says why. Enforced on website-godolint by its `external` recipe and `site/_headers`.

## UX

- **A table reflows on a phone, and stays a table.** Three columns do not fit in 375px,
  and a table scrolled sideways is unreadable: the reader drags every row to finish each
  sentence, and the row's key scrolls out of view while they do. Below the breakpoint
  each row stacks, the short cells on one line and the long one beneath, and the header
  row is hidden from sight only. The trap is that the stacking is done by changing the
  `display` of the table elements, and Chrome and Safari then drop the table from the
  accessibility tree: a screen reader hears a run of text with no rows, no columns and no
  headers. So the markup restates what the element already meant, `role="table"`,
  `rowgroup`, `row`, `columnheader` and `cell`, on every table that reflows; redundant at
  desktop width, the only thing holding the semantics at phone width. Verified by reading
  the accessibility tree at phone width, not by looking: a `table` with `cell` children
  must still be there. Enforced on website-godolint in `site/style.css` (the
  `max-width: 600px` block) and on both rules tables.
