package turso

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory/compose"
)

// benchmarkVocabulary is 400 synthetic words, mostly Cyrillic, so one ~500
// byte text holds a given word with roughly 12% probability.
func benchmarkVocabulary(random *rand.Rand) []string {
	cyrillic := []string{"па", "ме", "то", "ри", "ка", "ло", "ст", "на", "ве", "де", "зи", "му", "ще", "жа", "ци", "бо"}
	latin := []string{"ro", "ta", "sy", "ne", "go", "db", "li", "ux"}
	words := make([]string, 0, 400)
	for len(words) < cap(words) {
		syllables := cyrillic
		if len(words)%4 == 3 {
			syllables = latin
		}
		var word strings.Builder
		for range 2 + random.IntN(3) {
			word.WriteString(syllables[random.IntN(len(syllables))])
		}
		words = append(words, word.String())
	}
	return words
}

func benchmarkText(random *rand.Rand, words []string) string {
	var text strings.Builder
	for text.Len() < 500 {
		if text.Len() > 0 {
			text.WriteByte(' ')
		}
		word := words[random.IntN(len(words))]
		if random.IntN(10) == 0 {
			word = strings.ToUpper(word)
		}
		text.WriteString(word)
	}
	return text.String()
}

// seedBenchmarkMemory writes 20 000 current versions: 5 000 in "project-0" and
// 5 000 in each of three other projects.
func seedBenchmarkMemory(b *testing.B, store *Store) []string {
	b.Helper()
	ctx := context.Background()
	random := rand.New(rand.NewPCG(12, 2026))
	words := benchmarkVocabulary(random)
	err := inTransaction(ctx, store.db, func(tx database) error {
		for index := range 20000 {
			id := fmt.Sprintf("record-%05d", index)
			project := fmt.Sprintf("project-%d", index%4)
			text := benchmarkText(random, words)
			created := at(index)
			if _, err := tx.ExecContext(ctx, "INSERT INTO memory_records (id, project_id, category, status, current_version_id, version, created_at, updated_at) VALUES (?, ?, 'architecture_decision', 'active', ?, 1, ?, ?)", id, project, id+"-v1", created, created); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_record_versions (id, record_id, project_id, content, content_hash, search_text, search_normalization_version, author, trust, redaction_policy_version, visibility, origin, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'martin', 'human_approved', '1', 'project', 'human', ?)`, id+"-v1", id, project, text, "sha256:"+id, normalizeSearchText(text), searchNormalizationVersion, created); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return words
}

// BenchmarkSearchRecords measures one search over a 5 000-version project
// with ScanLimit 5 000 and a two-term query: common terms (many hits to load)
// and rare terms (scan cost only), without and with an ordering index.
func BenchmarkSearchRecords(b *testing.B) {
	store, err := Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	words := seedBenchmarkMemory(b, store)
	queries := map[string]string{"common": words[0] + " " + words[1], "rare": "отсъстващ missing"}
	run := func(name string) {
		for _, kind := range []string{"common", "rare"} {
			b.Run(name+"/"+kind, func(b *testing.B) {
				reader := store.MemoryReader()
				query := compose.SearchQuery{ProjectID: "project-0", Text: queries[kind], ScanLimit: 5000}
				var hits int
				for b.Loop() {
					result, err := reader.SearchRecords(context.Background(), query)
					if err != nil {
						b.Fatal(err)
					}
					if result.LimitReached {
						b.Fatal("limit reached")
					}
					hits = len(result.Hits)
				}
				b.ReportMetric(float64(hits), "hits")
			})
		}
	}
	run("no-index")
	if _, err := store.db.ExecContext(context.Background(), "CREATE INDEX bench_versions_recency ON memory_record_versions (project_id, created_at, id)"); err != nil {
		b.Fatal(err)
	}
	run("recency-index")
}
