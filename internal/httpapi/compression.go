package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type compressedResponseWriter struct {
	http.ResponseWriter
	gzipWriter *gzip.Writer
	statusCode int
	decided    bool
	compressed bool
}

func (s *Server) compression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appendVaryHeader(w.Header(), "Accept-Encoding")
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) ||
			strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		writer := &compressedResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(writer, r)
		if writer.gzipWriter != nil {
			_ = writer.gzipWriter.Close()
		}
	})
}

func (w *compressedResponseWriter) WriteHeader(statusCode int) {
	if w.decided {
		return
	}
	w.statusCode = statusCode
	w.decide()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *compressedResponseWriter) Write(payload []byte) (int, error) {
	if !w.decided {
		if w.Header().Get("Content-Type") == "" && len(payload) > 0 {
			w.Header().Set("Content-Type", http.DetectContentType(payload))
		}
		w.decide()
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	if w.compressed {
		return w.gzipWriter.Write(payload)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *compressedResponseWriter) Flush() {
	if !w.decided {
		w.decide()
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *compressedResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{w}, reader)
}

func (w *compressedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *compressedResponseWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	if w.statusCode < 200 || w.statusCode == http.StatusNoContent || w.statusCode == http.StatusNotModified ||
		w.Header().Get("Content-Encoding") != "" || !compressibleContentType(w.Header().Get("Content-Type")) {
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	w.gzipWriter = gzip.NewWriter(w.ResponseWriter)
	w.compressed = true
}

func acceptsGzip(value string) bool {
	var explicitSeen, explicitAccepted, explicitInvalid bool
	var wildcardSeen, wildcardAccepted, wildcardInvalid bool
	for _, part := range strings.Split(value, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		encoding := strings.TrimSpace(segments[0])
		if !strings.EqualFold(encoding, "gzip") && encoding != "*" {
			continue
		}
		accepted, valid := acceptsEncodingQuality(segments[1:])
		if strings.EqualFold(encoding, "gzip") {
			explicitSeen = true
			explicitAccepted = explicitAccepted || accepted
			explicitInvalid = explicitInvalid || !valid
			continue
		}
		wildcardSeen = true
		wildcardAccepted = wildcardAccepted || accepted
		wildcardInvalid = wildcardInvalid || !valid
	}
	if explicitSeen {
		return !explicitInvalid && explicitAccepted
	}
	return wildcardSeen && !wildcardInvalid && wildcardAccepted
}

func acceptsEncodingQuality(parameters []string) (bool, bool) {
	accepted := true
	qualitySeen := false
	for _, parameter := range parameters {
		key, raw, found := strings.Cut(strings.TrimSpace(parameter), "=")
		if !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}
		if !found || qualitySeen {
			return false, false
		}
		qualitySeen = true
		var valid bool
		accepted, valid = parseEncodingQuality(strings.TrimSpace(raw))
		if !valid {
			return false, false
		}
	}
	return accepted, true
}

func parseEncodingQuality(value string) (bool, bool) {
	integer, fraction, decimal := strings.Cut(value, ".")
	if integer != "0" && integer != "1" {
		return false, false
	}
	if !decimal {
		return integer == "1", true
	}
	if len(fraction) > 3 {
		return false, false
	}
	positive := integer == "1"
	for _, digit := range fraction {
		if digit < '0' || digit > '9' || integer == "1" && digit != '0' {
			return false, false
		}
		positive = positive || digit != '0'
	}
	return positive, true
}

func compressibleContentType(value string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") ||
		mediaType == "application/javascript" || mediaType == "application/xml" ||
		mediaType == "image/svg+xml"
}

func appendVaryHeader(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
