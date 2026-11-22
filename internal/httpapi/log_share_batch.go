package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type stagedFileLogShare struct {
	preparedFileLogShare
	Index        int
	PublicFileID string
}

// Every existing per-file bound is retained. Stage only already-redacted text
// privately, so ten accepted ZIPs do not multiply the live in-memory payload.
// Provider reads/redaction finish before the single business transaction begins.
func (s *Server) createFileLogShareBatch(r *http.Request, publicIDs []string, expiresAt time.Time) ([]map[string]any, error) {
	owner := currentClaims(r).Subject
	items := make([]map[string]any, len(publicIDs))
	prepared := make([]stagedFileLogShare, 0, len(publicIDs))
	var directory string
	var directoryInfo os.FileInfo
	defer func() {
		if directory == "" {
			return
		}
		if directoryInfo == nil {
			slog.Error("sanitized log stage ownership was not established; cleanup refused")
			return
		}
		current, err := os.Lstat(directory)
		if err != nil && !os.IsNotExist(err) {
			slog.Error("sanitized log stage ownership check failed")
			return
		}
		if os.IsNotExist(err) {
			return
		}
		if !current.IsDir() || current.Mode().Perm()&0077 != 0 || !os.SameFile(directoryInfo, current) {
			slog.Error("sanitized log stage ownership changed; cleanup refused")
			return
		}
		if err = os.RemoveAll(directory); err != nil {
			slog.Error("sanitized log stage cleanup failed")
		}
	}()
	for index, publicID := range publicIDs {
		items[index] = map[string]any{"fileId": publicID, "status": "failed"}
		p, err := prepareFileLogShare(r.Context(), s.db, s.readLogOSSObject, owner, publicID)
		if err != nil {
			items[index]["error"] = err.Error()
			continue
		}
		staged := stagedFileLogShare{preparedFileLogShare: p, Index: index, PublicFileID: publicID}
		if !p.AlreadyReady {
			if directory == "" {
				directory, err = os.MkdirTemp("", "mcmods-sanitized-log-batch-")
				if err != nil {
					return nil, err
				}
				directoryInfo, err = os.Lstat(directory)
				if err != nil {
					return nil, err
				}
			}
			for entryIndex := range staged.Entries {
				name := filepath.Join(directory, fmt.Sprintf("%d-%d.txt", index, entryIndex))
				file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return nil, err
				}
				_, writeErr := file.WriteString(staged.Entries[entryIndex].Text)
				closeErr := file.Close()
				if writeErr != nil {
					return nil, writeErr
				}
				if closeErr != nil {
					return nil, closeErr
				}
				staged.TextPaths = append(staged.TextPaths, name)
				staged.Entries[entryIndex].Text = ""
			}
		}
		prepared = append(prepared, staged)
	}
	if len(prepared) == 0 {
		skipRequestActivity(r)
		return items, nil
	}
	// All batches acquire source/unique-share locks in the same order, independent
	// of input order. Result slots retain the caller's original order and aliases.
	sort.SliceStable(prepared, func(i, j int) bool { return prepared[i].FileID < prepared[j].FileID })
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(r.Context())
	created := 0
	for _, p := range prepared {
		share, saveErr := publishFileLogShareTx(r.Context(), tx, owner, p.preparedFileLogShare, expiresAt)
		var itemErr fileLogShareConflict
		if errors.As(saveErr, &itemErr) {
			items[p.Index]["error"] = itemErr.Error()
			continue
		}
		if saveErr != nil {
			return nil, saveErr
		}
		if share.created {
			created++
		}
		items[p.Index] = share.response
		items[p.Index]["fileId"] = p.PublicFileID
	}
	if created == 0 {
		skipRequestActivity(r)
	} else if err = enqueueRequestActivityTx(r, tx); err != nil {
		return nil, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return nil, err
	}
	return items, nil
}
