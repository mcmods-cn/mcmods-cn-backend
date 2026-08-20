package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type reviewAttachmentQueryStub struct {
	query string
	args  []any
}

func (stub *reviewAttachmentQueryStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	stub.query = query
	stub.args = args
	return reviewAttachmentRowStub{}
}

type reviewAttachmentRowStub struct{}

func (reviewAttachmentRowStub) Scan(destinations ...any) error {
	*destinations[0].(*int64) = 42
	*destinations[1].(*string) = "abc123def"
	*destinations[2].(*string) = "users/7/proof.png"
	*destinations[3].(*string) = "proof.png"
	*destinations[4].(*int64) = 128
	return nil
}

func TestLookupReviewAttachmentEnforcesSafeOSSState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		lookup            reviewAttachmentLookup
		wantQueryFragment string
		wantForUpdate     bool
		wantFirstArg      any
	}{
		{
			name:              "uploader",
			lookup:            reviewAttachmentLookup{Kind: reviewAttachmentByUploader, UploaderID: 7},
			wantQueryFragment: "from oss_files file",
			wantForUpdate:     true,
			wantFirstArg:      "abc123def",
		},
		{
			name:              "creator claim",
			lookup:            reviewAttachmentLookup{Kind: reviewAttachmentForCreatorClaim, SubjectPublicID: "xyz987uvw"},
			wantQueryFragment: "from creator_claim_attachments attachment",
			wantFirstArg:      "xyz987uvw",
		},
		{
			name:              "minecraft server",
			lookup:            reviewAttachmentLookup{Kind: reviewAttachmentForMinecraftServer, SubjectPublicID: "xyz987uvw"},
			wantQueryFragment: "from minecraft_server_proof_files proof",
			wantFirstArg:      "xyz987uvw",
		},
		{
			name:              "project editor application",
			lookup:            reviewAttachmentLookup{Kind: reviewAttachmentForProjectEditorApplication, SubjectPublicID: "xyz987uvw"},
			wantQueryFragment: "from project_editor_application_attachments attachment",
			wantFirstArg:      "xyz987uvw",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stub := &reviewAttachmentQueryStub{}
			file, err := lookupReviewAttachment(context.Background(), stub, test.lookup, "ABC123DEF")
			if err != nil {
				t.Fatalf("lookupReviewAttachment() error = %v", err)
			}
			if file.InternalID != 42 || file.PublicID != "abc123def" || file.ObjectKey != "users/7/proof.png" || file.SizeBytes != 128 {
				t.Fatalf("lookupReviewAttachment() file = %#v", file)
			}
			normalizedQuery := strings.Join(strings.Fields(stub.query), " ")
			for _, fragment := range []string{
				test.wantQueryFragment,
				"file.status='active'",
				"file.scan_status in ('clean','trusted_generated')",
			} {
				if !strings.Contains(normalizedQuery, fragment) {
					t.Errorf("query %q does not contain %q", normalizedQuery, fragment)
				}
			}
			if strings.Contains(normalizedQuery, "for update") != test.wantForUpdate {
				t.Errorf("query FOR UPDATE = %v, want %v", strings.Contains(normalizedQuery, "for update"), test.wantForUpdate)
			}
			if len(stub.args) == 0 || stub.args[0] != test.wantFirstArg {
				t.Errorf("first query argument = %#v, want %#v", stub.args, test.wantFirstArg)
			}
		})
	}
}

func TestLookupReviewAttachmentRejectsInvalidLookup(t *testing.T) {
	t.Parallel()
	stub := &reviewAttachmentQueryStub{}
	if _, err := lookupReviewAttachment(context.Background(), stub, reviewAttachmentLookup{}, "abc123def"); err == nil {
		t.Fatal("lookupReviewAttachment() accepted an unsupported lookup kind")
	}
	if stub.query != "" {
		t.Fatal("lookupReviewAttachment() queried the database for an unsupported lookup kind")
	}
}
