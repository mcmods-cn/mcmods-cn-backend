package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var errOSSFileNotBindable = errors.New("OSS raster file is not bindable")

var safeRasterContentTypes = []string{
	"image/png",
	"image/apng",
	"image/jpeg",
	"image/jpg",
	"image/gif",
	"image/webp",
}

type ossRasterBindingScope struct {
	UploaderID       int64
	AllowAnyUploader bool
}

type trustedRasterOSSFile struct {
	ID           int64
	PublicID     string
	UploaderID   int64
	ObjectKey    string
	OriginalName string
	ContentType  string
	SizeBytes    int64
	Status       string
	ScanStatus   string
}

// resolveTrustedRasterOSSFilePublicID is the single binding boundary for OSS
// images. Asset handlers must not bind a bare oss_files ID: the file must still
// be active, belong to the expected uploader unless a separately-authorized
// workflow explicitly allows reuse, have a trusted scan result, and use a
// raster MIME type. SVG is intentionally excluded.
func resolveTrustedRasterOSSFilePublicID(
	ctx context.Context,
	query revisionQuery,
	publicID string,
	scope ossRasterBindingScope,
) (trustedRasterOSSFile, error) {
	var file trustedRasterOSSFile
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if !validCatalogPublicID(publicID) {
		return file, fmt.Errorf("%w: invalid public ID", errOSSFileNotBindable)
	}
	if !scope.AllowAnyUploader && scope.UploaderID <= 0 {
		return file, fmt.Errorf("%w: uploader is required", errOSSFileNotBindable)
	}
	err := query.QueryRow(ctx, `select id,public_id,coalesce(uploader_id,0),object_key,original_name,
		content_type,size_bytes,status,scan_status
		from oss_files
		where public_id=$1 and status='active'
		  and scan_status in ('clean','trusted_generated')
		  and lower(trim(split_part(content_type,';',1)))=any($4::text[])
		  and ($2 or uploader_id=$3)`,
		publicID, scope.AllowAnyUploader, scope.UploaderID, safeRasterContentTypes,
	).Scan(
		&file.ID,
		&file.PublicID,
		&file.UploaderID,
		&file.ObjectKey,
		&file.OriginalName,
		&file.ContentType,
		&file.SizeBytes,
		&file.Status,
		&file.ScanStatus,
	)
	if err != nil {
		return trustedRasterOSSFile{}, err
	}
	// Repeat the security assertions in Go. Besides making the boundary easy to
	// test, this prevents an alternative revisionQuery implementation from
	// accidentally weakening the SQL predicate.
	if file.Status != "active" || !trustedOSSScanStatus(file.ScanStatus) || !safeRasterContentType(file.ContentType) {
		return trustedRasterOSSFile{}, errOSSFileNotBindable
	}
	if !scope.AllowAnyUploader && file.UploaderID != scope.UploaderID {
		return trustedRasterOSSFile{}, errOSSFileNotBindable
	}
	return file, nil
}

func resolveOptionalTrustedRasterOSSFilePublicID(
	ctx context.Context,
	query revisionQuery,
	publicID *string,
	scope ossRasterBindingScope,
) (*int64, error) {
	if publicID == nil || strings.TrimSpace(*publicID) == "" {
		return nil, nil
	}
	file, err := resolveTrustedRasterOSSFilePublicID(ctx, query, *publicID, scope)
	if err != nil {
		return nil, err
	}
	return &file.ID, nil
}

func trustedOSSScanStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "clean", "trusted_generated":
		return true
	default:
		return false
	}
}

func safeRasterContentType(value string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	for _, allowed := range safeRasterContentTypes {
		if mediaType == allowed {
			return true
		}
	}
	return false
}
