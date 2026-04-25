package embedding

import (
	"context"
	"math"
	"testing"
)

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func TestTFIDFProvider_SimilarTexts(t *testing.T) {
	p := NewTFIDFProvider(384)
	ctx := context.Background()

	// Seed some documents to build IDF vocabulary.
	docs := []string{
		"func handleError(err error) { log.Error(err) }",
		"func processRequest(req *http.Request) error { return nil }",
		"func main() { fmt.Println(\"hello world\") }",
		"type ErrorHandler struct { logger *slog.Logger }",
		"func (h *ErrorHandler) Handle(err error) { h.logger.Error(err.Error()) }",
	}
	for _, d := range docs {
		p.Learn(d)
	}

	// Now test similarity.
	a, _ := p.Embed(ctx, "error handler function")
	b, _ := p.Embed(ctx, "handle errors in function")
	c, _ := p.Embed(ctx, "database connection pool timeout")

	simAB := cosineSimilarity(a, b)
	simAC := cosineSimilarity(a, c)

	t.Logf("sim(error handler, handle errors) = %.4f", simAB)
	t.Logf("sim(error handler, database pool)  = %.4f", simAC)

	// Similar texts should score higher than unrelated ones.
	if simAB <= simAC {
		t.Errorf("expected sim(A,B)=%.4f > sim(A,C)=%.4f — similar texts should score higher", simAB, simAC)
	}
}

func TestTFIDFProvider_IdenticalTexts(t *testing.T) {
	p := NewTFIDFProvider(384)
	ctx := context.Background()

	// Learn some docs first to build vocabulary.
	p.Learn("func main() { fmt.Println(\"hello\") }")
	p.Learn("func other() { return nil }")

	a, _ := p.Embed(ctx, "func main() { fmt.Println(\"hello\") }")
	b, _ := p.Embed(ctx, "func main() { fmt.Println(\"hello\") }")

	sim := cosineSimilarity(a, b)
	if sim < 0.99 {
		t.Errorf("identical texts should have similarity ~1.0, got %.4f", sim)
	}
}

func TestTFIDFProvider_Dimensions(t *testing.T) {
	p := NewTFIDFProvider(256)
	if p.Dimensions() != 256 {
		t.Errorf("dims = %d, want 256", p.Dimensions())
	}

	ctx := context.Background()
	vec, err := p.Embed(ctx, "test text")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 256 {
		t.Errorf("vector length = %d, want 256", len(vec))
	}
}

func TestTFIDFProvider_EmptyText(t *testing.T) {
	p := NewTFIDFProvider(384)
	vec, err := p.Embed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 384 {
		t.Errorf("vector length = %d, want 384", len(vec))
	}
	// Empty text should produce zero vector.
	for i, v := range vec {
		if v != 0 {
			t.Errorf("vec[%d] = %f, want 0 for empty text", i, v)
			break
		}
	}
}

func TestTFIDFProvider_ConcurrentSafety(t *testing.T) {
	p := NewTFIDFProvider(384)

	done := make(chan struct{})
	for range 50 {
		go func() {
			defer func() { done <- struct{}{} }()
			p.Learn("concurrent test text with some variation")
		}()
	}
	for range 50 {
		<-done
	}

	if p.DocCount() != 50 {
		t.Errorf("docCount = %d, want 50", p.DocCount())
	}
}

func TestTFIDFProvider_CodeSimilarity(t *testing.T) {
	p := NewTFIDFProvider(384)
	ctx := context.Background()

	// Seed vocabulary with code snippets.
	snippets := []string{
		"func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }",
		"func WriteFile(path string, data []byte) error { return os.WriteFile(path, data, 0644) }",
		"func ListFiles(dir string) ([]string, error) { entries, err := os.ReadDir(dir) }",
		"func ConnectDB(dsn string) (*sql.DB, error) { return sql.Open(\"postgres\", dsn) }",
		"func QueryRows(db *sql.DB, query string) (*sql.Rows, error) { return db.Query(query) }",
	}
	for _, s := range snippets {
		p.Learn(s)
	}

	fileOp, _ := p.Embed(ctx, "read file from disk path")
	dbOp, _ := p.Embed(ctx, "query database rows")

	readFile, _ := p.Embed(ctx, "func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }")
	queryDB, _ := p.Embed(ctx, "func QueryRows(db *sql.DB, query string) (*sql.Rows, error) { return db.Query(query) }")

	simFileRead := cosineSimilarity(fileOp, readFile)
	simFileDB := cosineSimilarity(fileOp, queryDB)
	simDBQuery := cosineSimilarity(dbOp, queryDB)
	simDBFile := cosineSimilarity(dbOp, readFile)

	t.Logf("sim(file_op, ReadFile)  = %.4f", simFileRead)
	t.Logf("sim(file_op, QueryDB)   = %.4f", simFileDB)
	t.Logf("sim(db_op, QueryDB)     = %.4f", simDBQuery)
	t.Logf("sim(db_op, ReadFile)    = %.4f", simDBFile)

	// File operations should be more similar to file code than DB code.
	if simFileRead <= simFileDB {
		t.Errorf("expected file_op closer to ReadFile than QueryDB")
	}
	// DB operations should be more similar to DB code than file code.
	if simDBQuery <= simDBFile {
		t.Errorf("expected db_op closer to QueryDB than ReadFile")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string // expected unigrams (bigrams also generated but not checked exhaustively)
	}{
		{"errorHandler", []string{"error", "handler"}},
		{"snake_case_name", []string{"snake", "case", "name"}},
		{"HTTPServer", []string{"httpserver"}}, // all-caps prefix stays together
		{"func main()", []string{"func", "main"}},
		{"a b", nil}, // single chars skipped
		{"", nil},
	}

	for _, tt := range tests {
		terms := tokenize(tt.input)
		// Check that expected unigrams are present.
		termSet := make(map[string]bool, len(terms))
		for _, t := range terms {
			termSet[t] = true
		}
		for _, w := range tt.want {
			if !termSet[w] {
				t.Errorf("tokenize(%q): missing term %q in %v", tt.input, w, terms)
			}
		}
	}
}
