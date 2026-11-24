package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

var (
	errContentTranslationModel    = errors.New("content translation model is unavailable")
	errContentTranslationSize     = errors.New("the content cannot be translated within the configured model limits")
	errContentTranslationActivity = errors.New("failed to persist AI translation activity")
)

func writeContentTranslationEnqueueError(w http.ResponseWriter, subjectID string, err error) {
	switch {
	case errors.Is(err, errAIQuotaExceeded):
		writeError(w, http.StatusTooManyRequests, "daily AI token allowance is insufficient")
	case errors.Is(err, errContentTranslationModel):
		writeError(w, http.StatusServiceUnavailable, errContentTranslationModel.Error())
	case errors.Is(err, errContentTranslationSize):
		writeError(w, http.StatusServiceUnavailable, errContentTranslationSize.Error())
	default:
		slog.Error("could not queue content translation", "subject_id", subjectID, "error_class", fmt.Sprintf("%T", err))
		if errors.Is(err, errContentTranslationActivity) {
			writeError(w, http.StatusServiceUnavailable, errContentTranslationActivity.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "Could not confirm the translation request. Check its status before submitting again.")
	}
}

// The worker keeps its diagnostic in ai_tasks.error. User-facing polling only
// reports the recorded lifecycle; a failed task can have an unsettled charge.
func publicTranslationTaskError(status string) string {
	if status == "failed" {
		return "Translation failed. Contact an administrator with the task ID."
	}
	return ""
}
