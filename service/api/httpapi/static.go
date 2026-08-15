package httpapi

import (
	"io/fs"
	"net/http"
	"strings"

	"empirebus-tests/service/buildinfo"
	webui "empirebus-tests/web"
)

func registerStaticRoutes(mux *http.ServeMux) {
	staticFS, err := fs.Sub(webui.Static, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/ui" {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			http.Error(w, "load web UI", http.StatusInternalServerError)
			return
		}
		version := buildinfo.Current().GitSHA
		body := strings.ReplaceAll(string(index), `href="/static/styles.css"`, `href="/static/styles.css?v=`+version+`"`)
		body = strings.ReplaceAll(body, `src="/static/navigation.js"`, `src="/static/navigation.js?v=`+version+`"`)
		body = strings.ReplaceAll(body, `src="/static/app.js"`, `src="/static/app.js?v=`+version+`"`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body))
	})
}
