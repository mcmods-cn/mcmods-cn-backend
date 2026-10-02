package database

import (
	"strings"
	"testing"
)

func TestBUG031ServerModEvidenceUsesGeneration145MultiSourceSchema(t *testing.T) {
	if schemaGeneration != 168 {
		t.Fatalf("schema generation=%d want 168 for independent server-mod evidence", schemaGeneration)
	}
	schema := strings.ToLower(strings.Join(serverSchemaStatements(), "\n"))
	for _, required := range []string{
		"create table minecraft_server_mod_evidence",
		"server_mod_id bigint not null references minecraft_server_mods(id) on delete cascade",
		"primary key(server_mod_id,source)",
		"create index idx_minecraft_server_mod_evidence_source",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("server schema lacks BUG031 multi-source invariant %q", required)
		}
	}
}
