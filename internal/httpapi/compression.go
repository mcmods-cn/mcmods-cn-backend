package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
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
	for _, part := range strings.Split(value, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(segments[0]), "gzip") && strings.TrimSpace(segments[0]) != "*" {
			continue
		}
		accepted := true
		for _, parameter := range segments[1:] {
			key, raw, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
				accepted = err != nil || quality > 0
			}
		}
		if accepted {
			return true
		}
	}
	return false
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
