package tutorial

import (
	"fmt"
	"html"
	"path"
	"strings"
)

// HTMLOptions configures standalone HTML guide rendering.
type HTMLOptions struct {
	VideoPath  string
	IncludeTOC bool
}

// RenderHTML renders a self-contained single-file HTML5 illustrated tutorial
// guide with embedded dark-terminal CSS, zero external links, optional video
// playback, optional sticky Table of Contents, and ANSI-rendered terminal
// frames for every captured step.
func RenderHTML(res Result, opts HTMLOptions) string {
	var b strings.Builder
	b.Grow(16384)

	title := res.Spec.Title
	if title == "" {
		title = res.Spec.Tool
	}
	if title == "" {
		title = "Tutorial"
	}

	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("  <meta charset=\"utf-8\">\n")
	b.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "  <title>%s</title>\n", html.EscapeString(title))
	b.WriteString("  <style>\n")
	b.WriteString(guideCSS)
	b.WriteString("  </style>\n")
	b.WriteString("</head>\n<body>\n")

	b.WriteString("<div class=\"page\">\n")
	b.WriteString("  <header class=\"hero\">\n")
	if res.Spec.Tool != "" {
		fmt.Fprintf(&b, "    <div class=\"tool-badge\">%s</div>\n", html.EscapeString(res.Spec.Tool))
	}
	fmt.Fprintf(&b, "    <h1>%s</h1>\n", html.EscapeString(title))
	if res.Spec.Description != "" {
		fmt.Fprintf(&b, "    <p class=\"description\">%s</p>\n", html.EscapeString(res.Spec.Description))
	}
	if res.Spec.Command != "" {
		fmt.Fprintf(&b, "    <div class=\"launch-cmd\"><span class=\"prompt\">$</span><code>%s</code></div>\n", html.EscapeString(res.Spec.Command))
	}
	b.WriteString("  </header>\n")

	if opts.VideoPath != "" {
		mime := videoMIME(opts.VideoPath)
		b.WriteString("  <section class=\"video-panel\" aria-label=\"Walkthrough video\">\n")
		b.WriteString("    <video controls playsinline preload=\"metadata\">\n")
		fmt.Fprintf(&b, "      <source src=\"%s\" type=\"%s\">\n", html.EscapeString(opts.VideoPath), mime)
		b.WriteString("      Your browser does not support embedded video playback.\n")
		b.WriteString("    </video>\n")
		b.WriteString("  </section>\n")
	}

	if opts.IncludeTOC && len(res.Sections) > 0 {
		b.WriteString("  <nav class=\"toc\" aria-label=\"Table of contents\">\n")
		b.WriteString("    <div class=\"toc-title\">Contents</div>\n")
		b.WriteString("    <ol>\n")
		for i, sec := range res.Sections {
			fmt.Fprintf(&b, "      <li><a href=\"#%s\"><span class=\"toc-num\">%d.</span> %s</a></li>\n",
				html.EscapeString(sec.ID), i+1, html.EscapeString(sec.Title))
		}
		b.WriteString("    </ol>\n")
		b.WriteString("  </nav>\n")
	}

	b.WriteString("  <main class=\"guide\">\n")
	displayStep := 0
	for i, sec := range res.Sections {
		fmt.Fprintf(&b, "    <section id=\"%s\" class=\"guide-section\">\n", html.EscapeString(sec.ID))
		b.WriteString("      <div class=\"section-header\">\n")
		fmt.Fprintf(&b, "        <span class=\"section-num\">%02d</span>\n", i+1)
		fmt.Fprintf(&b, "        <h2>%s</h2>\n", html.EscapeString(sec.Title))
		b.WriteString("      </div>\n")
		if sec.Goal != "" {
			fmt.Fprintf(&b, "      <p class=\"section-goal\"><strong>Goal:</strong> %s</p>\n", html.EscapeString(sec.Goal))
		}

		for _, step := range sec.Steps {
			if !step.Captured {
				continue
			}
			displayStep++
			b.WriteString("      <article class=\"step-card\">\n")
			b.WriteString("        <div class=\"step-header\">\n")
			b.WriteString("          <div class=\"step-title-row\">\n")
			fmt.Fprintf(&b, "            <span class=\"step-num\">Step %d</span>\n", displayStep)
			if step.Caption != "" {
				fmt.Fprintf(&b, "            <h3 class=\"step-caption\">%s</h3>\n", html.EscapeString(step.Caption))
			}
			b.WriteString("          </div>\n")

			hasKeys := len(step.Keys) > 0 || step.KeyBadge != ""
			if hasKeys || step.Command != "" {
				b.WriteString("          <div class=\"step-meta\">\n")
				if len(step.Keys) > 0 {
					b.WriteString("            <span class=\"key-badges\">")
					for _, k := range step.Keys {
						if k == "" {
							continue
						}
						fmt.Fprintf(&b, "<kbd>%s</kbd>", html.EscapeString(FormatKey(k)))
					}
					b.WriteString("</span>\n")
				} else if step.KeyBadge != "" {
					fmt.Fprintf(&b, "            <span class=\"key-badges\"><kbd>%s</kbd></span>\n", html.EscapeString(step.KeyBadge))
				}
				if step.Command != "" {
					fmt.Fprintf(&b, "            <span class=\"cmd-tag\">%s</span>\n", html.EscapeString(step.Command))
				}
				b.WriteString("          </div>\n")
			}
			b.WriteString("        </div>\n")

			b.WriteString("        <div class=\"term-window\">\n")
			b.WriteString("          <div class=\"term-chrome\" aria-hidden=\"true\"><span class=\"dot dot-r\"></span><span class=\"dot dot-y\"></span><span class=\"dot dot-g\"></span></div>\n")
			b.WriteString("          <div class=\"term-body\">")
			b.WriteString(ANSIToHTML(step.ANSI))
			b.WriteString("</div>\n")
			b.WriteString("        </div>\n")
			b.WriteString("      </article>\n")
		}

		b.WriteString("    </section>\n")
	}
	b.WriteString("  </main>\n")

	w := res.Spec.Width
	if w <= 0 {
		w = DefaultWidth
	}
	h := res.Spec.Height
	if h <= 0 {
		h = DefaultHeight
	}
	b.WriteString("  <footer class=\"guide-footer\">\n")
	if res.Spec.Theme != "" {
		fmt.Fprintf(&b, "    <span>%s &middot; %d&times;%d &middot; theme: %s</span>\n",
			html.EscapeString(res.Spec.Tool), w, h, html.EscapeString(res.Spec.Theme))
	} else {
		fmt.Fprintf(&b, "    <span>%s &middot; %d&times;%d</span>\n",
			html.EscapeString(res.Spec.Tool), w, h)
	}
	b.WriteString("  </footer>\n")
	b.WriteString("</div>\n")
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func videoMIME(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".webm":
		return "video/webm"
	case ".ogg", ".ogv":
		return "video/ogg"
	default:
		return "video/mp4"
	}
}

const guideCSS = `    :root {
      --bg: #0b0f14;
      --surface: #121821;
      --surface-2: #171f2b;
      --border: #263244;
      --text: #e6edf3;
      --muted: #8b949e;
      --accent: #58a6ff;
      --accent-soft: rgba(88, 166, 255, 0.14);
      --term-bg: #0d1117;
      --term-fg: #e6edf3;
      --kbd-bg: #1f2937;
      --kbd-border: #374151;
      --font-sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
      --font-mono: ui-monospace, "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
    }
    * { box-sizing: border-box; }
    html { scroll-behavior: smooth; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font-family: var(--font-sans);
      line-height: 1.55;
      -webkit-font-smoothing: antialiased;
    }
    .page {
      max-width: 1120px;
      margin: 0 auto;
      padding: 2rem 1.25rem 4rem;
    }
    .hero {
      padding: 1.75rem 2rem;
      background: linear-gradient(180deg, var(--surface-2), var(--surface));
      border: 1px solid var(--border);
      border-radius: 12px;
      margin-bottom: 1.5rem;
    }
    .tool-badge {
      display: inline-block;
      font-family: var(--font-mono);
      font-size: 0.78rem;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--accent);
      background: var(--accent-soft);
      border: 1px solid rgba(88, 166, 255, 0.3);
      padding: 0.2rem 0.6rem;
      border-radius: 999px;
      margin-bottom: 0.75rem;
    }
    .hero h1 {
      margin: 0 0 0.5rem;
      font-size: 1.9rem;
      line-height: 1.2;
      letter-spacing: -0.02em;
    }
    .description {
      margin: 0 0 1rem;
      color: var(--muted);
      font-size: 1rem;
      max-width: 72ch;
    }
    .launch-cmd {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.45rem 0.85rem;
      background: var(--term-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      font-family: var(--font-mono);
      font-size: 0.9rem;
    }
    .launch-cmd .prompt { color: #3fb950; user-select: none; }
    .video-panel {
      margin-bottom: 1.5rem;
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 0.75rem;
    }
    .video-panel video {
      display: block;
      width: 100%;
      border-radius: 8px;
      background: #000;
    }
    .toc {
      position: sticky;
      top: 0.75rem;
      z-index: 10;
      margin-bottom: 2rem;
      padding: 0.85rem 1.25rem;
      background: rgba(18, 24, 33, 0.92);
      backdrop-filter: blur(8px);
      border: 1px solid var(--border);
      border-radius: 10px;
    }
    .toc-title {
      font-size: 0.75rem;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      margin-bottom: 0.45rem;
    }
    .toc ol {
      list-style: none;
      margin: 0;
      padding: 0;
      display: flex;
      flex-wrap: wrap;
      gap: 0.45rem 1rem;
    }
    .toc a {
      color: var(--text);
      text-decoration: none;
      font-size: 0.92rem;
      padding: 0.2rem 0.45rem;
      border-radius: 6px;
      transition: background 0.15s ease, color 0.15s ease;
    }
    .toc a:hover {
      background: var(--accent-soft);
      color: var(--accent);
    }
    .toc-num { color: var(--accent); font-family: var(--font-mono); }
    .guide-section {
      margin-bottom: 3rem;
      scroll-margin-top: 5.5rem;
    }
    .section-header {
      display: flex;
      align-items: baseline;
      gap: 0.75rem;
      border-bottom: 1px solid var(--border);
      padding-bottom: 0.5rem;
      margin-bottom: 0.75rem;
    }
    .section-num {
      font-family: var(--font-mono);
      font-size: 0.85rem;
      color: var(--accent);
      background: var(--accent-soft);
      padding: 0.15rem 0.5rem;
      border-radius: 6px;
    }
    .section-header h2 {
      margin: 0;
      font-size: 1.4rem;
      letter-spacing: -0.01em;
    }
    .section-goal {
      margin: 0 0 1.25rem;
      color: var(--muted);
      font-size: 0.98rem;
    }
    .section-goal strong { color: var(--text); }
    .step-card {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 12px;
      margin-bottom: 1.5rem;
      overflow: hidden;
    }
    .step-header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.75rem;
      padding: 0.9rem 1.15rem;
      background: var(--surface-2);
      border-bottom: 1px solid var(--border);
    }
    .step-title-row {
      display: flex;
      align-items: baseline;
      gap: 0.65rem;
      min-width: 0;
    }
    .step-num {
      flex-shrink: 0;
      font-family: var(--font-mono);
      font-size: 0.78rem;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    .step-caption {
      margin: 0;
      font-size: 1rem;
      font-weight: 600;
    }
    .step-meta {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 0.5rem;
    }
    .key-badges {
      display: inline-flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 0.3rem;
    }
    kbd {
      display: inline-block;
      font-family: var(--font-mono);
      font-size: 0.82rem;
      line-height: 1.2;
      padding: 0.22rem 0.5rem;
      color: var(--text);
      background: var(--kbd-bg);
      border: 1px solid var(--kbd-border);
      border-bottom-width: 2px;
      border-radius: 6px;
      box-shadow: inset 0 -1px 0 rgba(0, 0, 0, 0.35);
    }
    .cmd-tag {
      font-family: var(--font-mono);
      font-size: 0.76rem;
      color: var(--muted);
      background: var(--term-bg);
      border: 1px solid var(--border);
      padding: 0.18rem 0.5rem;
      border-radius: 6px;
    }
    .term-window {
      background: var(--term-bg);
    }
    .term-chrome {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      padding: 0.45rem 0.85rem;
      background: #161b22;
      border-bottom: 1px solid #21262d;
    }
    .dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      display: inline-block;
    }
    .dot-r { background: #ff5f56; }
    .dot-y { background: #ffbd2e; }
    .dot-g { background: #27c93f; }
    .term-body {
      overflow-x: auto;
      padding: 0.85rem 1rem;
    }
    pre.term {
      margin: 0;
      font-family: var(--font-mono);
      font-size: 13px;
      line-height: 1.35;
      color: var(--term-fg);
      background: var(--term-bg);
      white-space: pre;
      tab-size: 4;
    }
    .guide-footer {
      margin-top: 2.5rem;
      padding-top: 1rem;
      border-top: 1px solid var(--border);
      color: var(--muted);
      font-family: var(--font-mono);
      font-size: 0.8rem;
      text-align: center;
    }
`
