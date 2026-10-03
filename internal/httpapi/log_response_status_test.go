package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOCT02AccessLogRecordsTheCommittedHTTPStatus(t *testing.T) {
	for _, bodyFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "implicit"}[bodyFirst], func(t *testing.T) {
			actual := httptest.NewRecorder()
			recorder := &responseRecorder{ResponseWriter: actual}
			if bodyFirst {
				_, _ = recorder.Write([]byte("ok"))
			} else {
				recorder.WriteHeader(http.StatusOK)
			}
			recorder.WriteHeader(http.StatusInternalServerError)
			if actual.Code != http.StatusOK || recorder.status != actual.Code {
				t.Fatalf("actual HTTP status=%d logged=%d", actual.Code, recorder.status)
			}
		})
	}
}
