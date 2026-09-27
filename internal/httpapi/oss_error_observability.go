package httpapi

import (
	"context"
	"log/slog"
	"sync/atomic"
)

type ossWriteFailureMetrics struct {
	UploadLogFailures       uint64 `json:"uploadLogFailures"`
	ScanLogFailures         uint64 `json:"scanLogFailures"`
	DownloadStatFailures    uint64 `json:"downloadStatFailures"`
	DeletionEnqueueFailures uint64 `json:"deletionEnqueueFailures"`
}

type ossWriteFailureCounters struct {
	uploadLog       atomic.Uint64
	scanLog         atomic.Uint64
	downloadStat    atomic.Uint64
	deletionEnqueue atomic.Uint64
}

func (c *ossWriteFailureCounters) snapshot() ossWriteFailureMetrics {
	return ossWriteFailureMetrics{
		UploadLogFailures:       c.uploadLog.Load(),
		ScanLogFailures:         c.scanLog.Load(),
		DownloadStatFailures:    c.downloadStat.Load(),
		DeletionEnqueueFailures: c.deletionEnqueue.Load(),
	}
}

func (s *Server) observeOSSWriteFailure(kind, objectKey string, err error) {
	if err == nil {
		return
	}
	switch kind {
	case "upload_log":
		s.ossWrites.uploadLog.Add(1)
	case "scan_log":
		s.ossWrites.scanLog.Add(1)
	case "download_stat":
		s.ossWrites.downloadStat.Add(1)
	case "deletion_enqueue":
		s.ossWrites.deletionEnqueue.Add(1)
	}
	slog.Error("persist OSS operational fact", "module", "oss", "kind", kind, "object_key", objectKey, "error", err)
}

func (s *Server) recordOSSScanLog(ctx context.Context, fileID int64, objectKey, result, message string) {
	_, err := s.db.Exec(ctx, `insert into oss_scan_logs (file_id, object_key, engine, result, message)
		values ($1, $2, 'manual', $3, $4)`, fileID, objectKey, result, message)
	if err != nil {
		s.observeOSSWriteFailure("scan_log", objectKey, err)
	}
}

func (s *Server) recordOSSDownloadStat(ctx context.Context, objectKey string) {
	_, err := s.db.Exec(ctx, `insert into oss_download_stats (object_key, downloads, last_download_at)
		values ($1, 1, now())
		on conflict (object_key) do update
		set downloads = oss_download_stats.downloads + 1, last_download_at = now()`, objectKey)
	if err != nil {
		s.observeOSSWriteFailure("download_stat", objectKey, err)
	}
}
