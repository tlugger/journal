journal
=======

A small, custom blog served from a Raspberry Pi at `blog.tylerkno.ws`. Posts
are markdown files in an Obsidian vault that already syncs to S3; the Pi
pulls the vault on a timer and a Go service renders posts on demand.

Companion to [notnottyler.com](https://notnottyler.com) — same earth-tone
palette and fonts, designed to be linked from there when the time comes.

Architecture
------------

```
Obsidian (Mac)  ──►  S3 bucket  ──►  Raspberry Pi (Caddy → Go service)
                                      ├─ aws s3 sync (every 5m, systemd timer)
                                      ├─ fsnotify wakes the renderer
                                      └─ in-memory cache of rendered HTML
```

- **Vault path the renderer looks at**: `$BLOG_VAULT_DIR/blog/`. Every
  subfolder with an `index.md` whose frontmatter has `published: true` is a
  post.
- **Caddy** terminates TLS and reverse-proxies `blog.tylerkno.ws` to
  `localhost:8106`. The installer does **not** touch the Caddyfile — see
  *Caddy setup* below.

Post format
-----------

A post is a folder under `vault/blog/` containing one `index.md` plus any
assets it references. Folder name is up to you (only `slug` controls the
URL).

```
vault/blog/
└── my-post/
    ├── index.md
    ├── image.png
    ├── template.html   # optional — full-page override
    └── style.css       # only used if template.html references it
```

Frontmatter:

```yaml
---
title: My post
slug: my-post
date: 2026-05-12
summary: One sentence shown on the index and in the RSS feed.
published: true
---
```

- `published: true` is required. Missing or `false` → invisible.
- `slug` becomes the URL (`/posts/<slug>`). Falls back to the folder name if
  omitted.
- `date` accepts `YYYY-MM-DD` or full RFC3339.
- Images and links use ordinary markdown — `![alt](image.png)` and
  `[text](other.html)`. Relative paths are rewritten to `/posts/<slug>/...`
  at render time so they Just Work in GitHub previews too.

### Obsidian + Templater setup

This uses the **Templater** community plugin (not the core Templates
plugin — that can't create folders). One-time setup, then "new post" is a
single command.

#### 1. Configure Templater

Open **Settings → Templater** and set:

- **Template folder location**: `Templates` (or wherever you keep
  templates — must match where the file in step 2 lives).
- **Trigger Templater on new file creation**: off (we'll invoke it
  explicitly so the prompt fires).
- Under **Folder Templates**, leave empty.

#### 2. Save the template file

Create `Templates/Blog Post.md` in your vault with **exactly** this
content. Everything is wrapped in one `<%* ... %>` block; output is
built up via Templater's `tR` accumulator so there are no cross-block
variable references that some Templater versions render as raw text.

```markdown
<%*
const slug = (await tp.system.prompt("Post slug (used as folder name + URL)")) || "";
const safe = slug.toLowerCase().trim().replace(/[^a-z0-9-]/g, "-").replace(/-+/g, "-").replace(/^-|-$/g, "");
if (!safe) { new Notice("Blog Post: aborted — empty slug"); return; }
await tp.file.move(`blog/${safe}/index`);
const today = tp.date.now("YYYY-MM-DD");
tR += `---
title: 
slug: ${safe}
date: ${today}
summary: 
published: false
---

`;
%>
```

#### 3. Configure Obsidian to paste images as standard markdown

The renderer uses ordinary `![alt](image.png)` (not Obsidian wikilinks),
so configure paste accordingly:

- **Settings → Files & Links → Use [[Wikilinks]]**: **off**
- **Settings → Files & Links → New link format**: **Relative path to
  file**
- **Settings → Files & Links → Default location for new attachments**:
  **In subfolder under current folder** (or **Same folder as current
  file**) — keeps pasted images co-located with the post.

#### 4. Write a post

1. Anywhere in the vault: open the command palette (`Cmd+P`) → run
   **Templater: Open insert template modal** → pick **Blog Post**.
2. Prompt asks for slug → type e.g. `my-post`.
3. Templater creates `blog/my-post/index.md`, drops you into it with the
   slug + today's date pre-filled.
4. Fill in `title`, `summary`, write your post. Paste images — they land
   alongside `index.md` as `image.png` etc.
5. When ready: flip `published: false` → `published: true`. Save. Your
   existing Obsidian→S3 sync ships it; the Pi picks it up on the next
   5-minute tick.

**Slug collisions:** if `blog/<slug>/` already exists, `tp.file.move`
will surface an Obsidian error. Pick a different slug and retry — the
template doesn't auto-disambiguate by design (silently appending a
number would be a surprise later).

Local development
-----------------

```sh
go test -race ./...                                    # unit tests
go run ./cmd/blog -vault ./testdata/vault -addr :8106  # smoke server
```

Then `open http://localhost:8106`. The committed `testdata/vault/` has
fixture posts that exercise the default template, a `template.html` override,
draft gating, and the folder-name slug fallback.

Pi deployment
-------------

```sh
curl -fsSL https://raw.githubusercontent.com/tlugger/journal/main/install.sh | sudo bash
```

The installer:

1. Drops a placeholder `/home/pi/blog/.env` on first install (S3 URI + AWS
   creds + `BLOG_SITE_URL`).
2. Detects architecture, fetches the latest release binary or builds from
   source if no release is published yet.
3. Installs `awscli` if missing.
4. Writes three systemd units:
   - `blog.service` — the Go server
   - `blog-vault-sync.service` — `aws s3 sync` (oneshot)
   - `blog-vault-sync.timer` — fires the sync every 5 minutes
5. Enables and starts them (only enables on first install, until you fill
   in `.env`).

### Caddy setup (manual, one-time)

The installer deliberately does not touch your Caddyfile. Add:

```
blog.tylerkno.ws {
    reverse_proxy localhost:8106
}
```

Then `sudo systemctl reload caddy`. Caddy provisions the TLS cert from
Let's Encrypt automatically given that DDNS already resolves the subdomain
to your Pi.

Repo layout
-----------

```
cmd/blog/main.go             # entrypoint: flags, fsnotify, http.Server
internal/post/                # frontmatter, vault walk, goldmark rendering
internal/server/              # routes, cache, handlers
internal/feed/                # hand-rolled RSS 2.0
templates/{base,index}.html   # default page chrome
static/base.css               # palette shared with notnottyler.com
testdata/vault/               # hermetic fixture used by every test
install.sh                    # curl|bash installer for the Pi
```

Tests are in `_test.go` files next to each source file (stdlib `testing`
only, table-driven, hermetic). `go test -race ./...` is the contract.
