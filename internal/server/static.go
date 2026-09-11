package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticHandler serves the website from a directory with clean URLs, sensible
// cache headers, and a custom 404 page. It is deliberately small; the site is
// a handful of static files.
type staticHandler struct {
	root string // absolute path, or "" if the dir doesn't exist
}

func newStaticHandler(dir string) http.Handler {
	h := &staticHandler{}
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				h.root = abs
			}
		}
	}
	return h
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Normalise the request path and refuse anything that tries to escape.
	upath := path.Clean("/" + r.URL.Path)
	if strings.Contains(upath, "..") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if h.root == "" {
		// No site deployed yet: answer "/" with a placeholder so the box is
		// obviously alive, 404 everything else.
		if upath == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write([]byte(placeholderHTML))
			return
		}
		h.notFound(w, r)
		return
	}

	if f, info, ok := h.resolve(upath); ok {
		defer f.Close()
		setCacheHeaders(w, info.Name())
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return
	}
	h.notFound(w, r)
}

// resolve maps a URL path to a file, trying a few clean-URL variants.
func (h *staticHandler) resolve(upath string) (*os.File, os.FileInfo, bool) {
	candidates := []string{upath}
	if upath == "/" {
		candidates = []string{"/index.html"}
	} else if path.Ext(upath) == "" {
		candidates = append(candidates, upath+".html", path.Join(upath, "index.html"))
	}

	for _, c := range candidates {
		full := filepath.Join(h.root, filepath.FromSlash(c))
		if !strings.HasPrefix(full, h.root+string(os.PathSeparator)) && full != h.root {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			continue
		}
		f, err := os.Open(full)
		if err != nil {
			continue
		}
		return f, info, true
	}
	return nil, nil, false
}

func (h *staticHandler) notFound(w http.ResponseWriter, r *http.Request) {
	if h.root != "" {
		full := filepath.Join(h.root, "404.html")
		if b, err := os.ReadFile(full); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusNotFound)
			w.Write(b)
			return
		}
	}
	http.Error(w, "404 - not found", http.StatusNotFound)
}

// setCacheHeaders picks a Cache-Control value from the file extension. There's
// no CDN in front, but repeat-visitor load still drops with a little caching.
func setCacheHeaders(w http.ResponseWriter, name string) {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		w.Header().Set("Cache-Control", "no-cache")
	case ".css", ".js", ".mjs", ".woff2", ".woff", ".ttf",
		".webp", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".avif":
		w.Header().Set("Cache-Control", "public, max-age=86400")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

const placeholderHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>kachel.solar</title>
<style>
  body{background:#0f380f;color:#F8FFDD;font-family:monospace;
       display:flex;min-height:100vh;margin:0;align-items:center;justify-content:center;text-align:center}
  code{background:#9bbc0f;color:#0f380f;padding:2px 6px}
</style>
<div>
  <p>&#10045; kachel's solar powered website &#10045;</p>
  <p>server is up &mdash; static site not deployed yet.</p>
  <p>drop the built site into the <code>-web</code> directory.</p>
  <p><a style="color:#9bbc0f" href="/api/battery">/api/battery</a> &middot;
     <a style="color:#9bbc0f" href="/api/sensor">/api/sensor</a></p>
</div>
</html>`
