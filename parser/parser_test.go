package parser

import (
	"testing"
)

func TestNewParser(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	if p == nil {
		t.Fatal("parser is nil")
	}
}

func TestParse(t *testing.T) {
	p, _ := NewParser()

	tests := []struct {
		name      string
		sql       string
		wantType  string
		wantDML   bool
		wantTable string
	}{
		{
			name:      "SELECT",
			sql:       "SELECT * FROM users WHERE id = 1",
			wantType:  "SELECT",
			wantDML:   false, // SELECT is not DML in vitess
			wantTable: "users",
		},
		{
			name:      "INSERT",
			sql:       "INSERT INTO users (name, email) VALUES ('test', 'test@test.com')",
			wantType:  "INSERT",
			wantDML:   true,
			wantTable: "users",
		},
		{
			name:      "UPDATE",
			sql:       "UPDATE users SET name = 'new' WHERE id = 1",
			wantType:  "UPDATE",
			wantDML:   true,
			wantTable: "users",
		},
		{
			name:      "DELETE",
			sql:       "DELETE FROM users WHERE id = 1",
			wantType:  "DELETE",
			wantDML:   true,
			wantTable: "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := p.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			if sig.Type != tt.wantType {
				t.Errorf("type = %s, want %s", sig.Type, tt.wantType)
			}

			if sig.IsDML != tt.wantDML {
				t.Errorf("isDML = %v, want %v", sig.IsDML, tt.wantDML)
			}

			foundTable := false
			for _, table := range sig.Tables {
				if table == tt.wantTable {
					foundTable = true
					break
				}
			}
			if !foundTable {
				t.Errorf("tables = %v, want to contain %s", sig.Tables, tt.wantTable)
			}

			if sig.Hash == "" {
				t.Error("hash is empty")
			}

			if sig.Structure == "" {
				t.Error("structure is empty")
			}
		})
	}
}

func TestMatch(t *testing.T) {
	p, _ := NewParser()

	tests := []struct {
		name           string
		query1         string
		query2         string
		wantExact      bool
		wantStructural bool
		minScore       int
	}{
		{
			name:           "exact match",
			query1:         "SELECT * FROM users WHERE id = 1",
			query2:         "SELECT * FROM users WHERE id = 1",
			wantExact:      true,
			wantStructural: true,
			minScore:       100,
		},
		{
			name:           "canonical match with reordered predicates",
			query1:         "SELECT * FROM users WHERE id = 1 AND status = 'active'",
			query2:         "select * from users where status = 'active' and id = 1",
			wantExact:      false,
			wantStructural: true,
			minScore:       80,
		},
		{
			name:           "different literal values do not match",
			query1:         "SELECT * FROM users WHERE id = 1",
			query2:         "SELECT * FROM users WHERE id = 2",
			wantExact:      false,
			wantStructural: false,
			minScore:       30,
		},
		{
			name:           "different columns do not match structurally",
			query1:         "SELECT id FROM users WHERE id = 1",
			query2:         "SELECT name FROM users WHERE id = 1",
			wantExact:      false,
			wantStructural: false,
			minScore:       30,
		},
		{
			name:           "different tables do not match structurally",
			query1:         "SELECT * FROM users WHERE id = 1",
			query2:         "SELECT * FROM orders WHERE id = 1",
			wantExact:      false,
			wantStructural: false,
			minScore:       30,
		},
		{
			name:           "completely different",
			query1:         "SELECT * FROM users",
			query2:         "INSERT INTO users (name) VALUES ('test')",
			wantExact:      false,
			wantStructural: false,
			minScore:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig1, _ := p.Parse(tt.query1)
			sig2, _ := p.Parse(tt.query2)

			exact, structural, score := p.Match(sig1, sig2)

			if exact != tt.wantExact {
				t.Errorf("exact = %v, want %v", exact, tt.wantExact)
			}

			if structural != tt.wantStructural {
				t.Errorf("structural = %v, want %v", structural, tt.wantStructural)
			}

			if score < tt.minScore {
				t.Errorf("score = %d, want >= %d", score, tt.minScore)
			}
		})
	}
}

func TestCaching(t *testing.T) {
	p, _ := NewParser()

	sql := "SELECT * FROM users WHERE id = 1"

	// First parse
	sig1, err := p.Parse(sql)
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	// Second parse should be cached
	sig2, err := p.Parse(sql)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	// Should be the same pointer (cached)
	if sig1 != sig2 {
		t.Error("expected cached signature to be returned")
	}
}

func TestGetQueryStructureCached(t *testing.T) {
	p, _ := NewParser()

	sql := "SELECT * FROM users WHERE id = 1"

	structure, err := p.GetQueryStructureCached(sql)
	if err != nil {
		t.Fatalf("failed to get structure: %v", err)
	}

	if structure == "" {
		t.Error("structure is empty")
	}

	// Should contain the canonical SQL text.
	if !contains(structure, "SELECT") {
		t.Errorf("structure should contain 'SELECT', got: %s", structure)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
