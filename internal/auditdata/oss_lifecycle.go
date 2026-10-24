package auditdata

// These are bounded triage sets, not a reconstruction of original HTTP intent.
// In particular upload status cannot prove whether a historical activity/reward
// is missing: old upload routes did not retain an exact business-to-event key.
func inspectOSSLifecycle(run func(string, string) error) error {
	for _, entry := range []struct{ name, query string }{
		{"oss_legacy_files_without_exact_historical_activity_linkage", `select count(*) over(),to_jsonb(public_id) from oss_files where created_at>=$1 and created_at<$2 order by id limit $3`},
		{"oss_report_evidence_metadata_repair_candidates", `select count(*) over(),to_jsonb(file.public_id) from oss_files file where file.source='report_evidence' and file.status='active' and file.uploader_id is not null and file.created_at>=$1 and file.created_at<$2 and not exists(select 1 from report_evidence evidence where evidence.object_key=file.object_key) order by file.id limit $3`},
		{"oss_expired_quota_reservation_candidates", `select count(*) over(),to_jsonb(owner.public_id) from oss_user_upload_quota_reservations reservation join users owner on owner.id=reservation.user_id where reservation.created_at>=$1 and reservation.created_at<$2 and reservation.expires_at<=now() order by reservation.user_id,reservation.object_key limit $3`},
		{"oss_dead_deletion_jobs", `select count(*) over(),to_jsonb(public_id) from oss_object_deletion_outbox where status='dead' and created_at>=$1 and created_at<$2 order by id limit $3`},
		{"oss_dead_rehome_jobs", `select count(*) over(),to_jsonb(public_id) from oss_rehome_jobs where status='dead' and created_at>=$1 and created_at<$2 order by id limit $3`},
		{"oss_dead_multipart_sessions", `select count(*) over(),to_jsonb(public_id) from oss_multipart_sessions where status='dead' and created_at>=$1 and created_at<$2 order by id limit $3`},
	} {
		if err := run(entry.name, entry.query); err != nil {
			return err
		}
	}
	return nil
}
