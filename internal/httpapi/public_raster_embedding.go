package httpapi

import "net/http"

// A public parent alone is not proof that its stored icon is public. Callers
// must establish the existing anonymous parent AND image authorization first.
// Only a successful raster redirect overrides the global same-site policy;
// errors, owner/reviewer-only previews and unrelated APIs keep their default.
type anonymousPublicRasterRedirectWriter struct {
	http.ResponseWriter
}

func (w anonymousPublicRasterRedirectWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w anonymousPublicRasterRedirectWriter) WriteHeader(status int) {
	if status == http.StatusTemporaryRedirect && validHTTPURL(w.Header().Get("Location")) {
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	}
	w.ResponseWriter.WriteHeader(status)
}

func anonymousPublicRasterRedirect(w http.ResponseWriter, allowed bool) http.ResponseWriter {
	if !allowed {
		return w
	}
	return anonymousPublicRasterRedirectWriter{ResponseWriter: w}
}
