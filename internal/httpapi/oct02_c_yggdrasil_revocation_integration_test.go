package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type oct02CRevokingUploadReader struct {
	reader io.Reader
	revoke func()
	read   bool
}

func (r *oct02CRevokingUploadReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		r.revoke()
	}
	return r.reader.Read(p)
}

// Revoke at the first multipart read, after real preflight authorization,
// before the write transaction. OSS is the existing owned HTTP test provider.
func TestOCT02CYggdrasilTextureUploadRejectsRevocationDuringBodyReadIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	userID := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, userID, yggdrasilLoginPermission)
	profile := f.profile(t, f.editor, "OCT02CProfile", "private")
	raw := test039PNG(t, 64, 64, 44)
	file := f.upload(t, f.editor, "oct02-c.png", raw)
	asset := f.asset(t, f.editor, file, "OCT02CAsset", "private")
	f.review(t, asset.PublicID, "approved")
	var profileID int64
	if err := f.db.QueryRow(f.ctx, `select id from player_profiles where public_id=$1`, profile.PublicID).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update yggdrasil_accounts set enabled=true where user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	accessToken := "oct02-c-test-only-launcher-token"
	if _, err := f.db.Exec(f.ctx, `insert into yggdrasil_tokens(access_token_hash,user_id,player_profile_id,client_token,status,expires_at) values($1,$2,$3,'oct02-c-client','active',now()+interval '1 hour')`, hashYggdrasilToken(accessToken), userID, profileID); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oct02-c.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]string{
		"revoked":           `update yggdrasil_tokens set status='revoked',revoked_at=now() where user_id=$1`,
		"expired":           `update yggdrasil_tokens set expires_at=now()-interval '1 second' where user_id=$1`,
		"launcher disabled": `update yggdrasil_accounts set enabled=false where user_id=$1`,
		"profile deleted":   `update player_profiles set status='deleted' where user_id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, reset := range []string{
				`update yggdrasil_tokens set status='active',revoked_at=null,expires_at=now()+interval '1 hour' where user_id=$1`,
				`update yggdrasil_accounts set enabled=true where user_id=$1`,
				`update player_profiles set status='active' where user_id=$1`,
			} {
				if _, resetErr := f.db.Exec(f.ctx, reset, userID); resetErr != nil {
					t.Fatal(resetErr)
				}
			}
			reader := &oct02CRevokingUploadReader{reader: bytes.NewReader(body.Bytes()), revoke: func() {
				if _, changeErr := f.db.Exec(f.ctx, change, userID); changeErr != nil {
					t.Fatal(changeErr)
				}
			}}
			request := httptest.NewRequest(http.MethodPut, "/api/yggdrasil/api/user/profile/"+profile.UUID+"/skin", reader).WithContext(f.ctx)
			request.SetPathValue("uuid", profile.UUID)
			request.SetPathValue("textureType", "skin")
			request.Header.Set("Authorization", "Bearer "+accessToken)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response := httptest.NewRecorder()
			f.server.yggdrasilSetTexture(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("invalidated upload status=%d body=%s", response.Code, response.Body.String())
			}
			var count int
			if readErr := f.db.QueryRow(f.ctx, `select count(*) from player_profile_textures where profile_id=$1`, profileID).Scan(&count); readErr != nil {
				t.Fatal(readErr)
			}
			if count != 0 {
				t.Fatalf("invalidated upload mutated profile textures=%d", count)
			}
		})
	}
}
