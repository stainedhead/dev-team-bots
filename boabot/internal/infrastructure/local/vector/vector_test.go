package vector_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
)

// helpers

func makeVec(dim int, vals ...float32) []float32 {
	v := make([]float32, dim)
	copy(v, vals)
	return v
}

func cosineSim(a, b []float32) float32 {
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(magA) * math.Sqrt(magB)))
}

// TestNew_CreatesDirectory verifies that New creates the vectors/ subdirectory.
func TestNew_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, err := vector.New(filepath.Join(dir, "botmem"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if vs == nil {
		t.Fatal("expected non-nil VectorStore")
	}
	info, err := os.Stat(filepath.Join(dir, "botmem", "vectors"))
	if err != nil {
		t.Fatalf("vectors/ dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("vectors/ is not a directory")
	}
}

// TestNew_BadPath verifies that New returns an error when the path cannot be created.
func TestNew_BadPath(t *testing.T) {
	t.Parallel()
	_, err := vector.New("/dev/null/impossible/path")
	if err == nil {
		t.Fatal("expected error for bad path, got nil")
	}
}

// TestNew_LoadsExistingVectors verifies that New loads pre-existing .vec/.meta files.
func TestNew_LoadsExistingVectors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "bot")

	// Write raw binary .vec file and JSON .meta file directly.
	if err := os.MkdirAll(filepath.Join(base, "vectors"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	vec := []float32{1.0, 0.0, 0.0}
	vecPath := filepath.Join(base, "vectors", "foo.vec")
	f, err := os.Create(vecPath)
	if err != nil {
		t.Fatalf("create vec file: %v", err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(vec))); err != nil {
		t.Fatalf("write dim: %v", err)
	}
	if err := binary.Write(f, binary.LittleEndian, vec); err != nil {
		t.Fatalf("write vec: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close vec file: %v", err)
	}

	type metaFile struct {
		Key      string            `json:"key"`
		Metadata map[string]string `json:"metadata"`
	}
	metaPath := filepath.Join(base, "vectors", "foo.meta")
	metaData, _ := json.Marshal(metaFile{Key: "foo", Metadata: map[string]string{"tag": "bar"}})
	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	vs, err := vector.New(base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := vs.Search(context.Background(), []float32{1.0, 0.0, 0.0}, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from loaded cache, got %d", len(results))
	}
	if results[0].Key != "foo" {
		t.Errorf("expected key=foo, got %q", results[0].Key)
	}
	if results[0].Metadata["tag"] != "bar" {
		t.Errorf("expected tag=bar, got %q", results[0].Metadata["tag"])
	}
}

// TestUpsert_WritesFilesAndUpdatesCache verifies Upsert writes .vec and .meta files.
func TestUpsert_WritesFilesAndUpdatesCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)

	ctx := context.Background()
	vec := makeVec(3, 1.0, 0.0, 0.0)
	meta := map[string]string{"src": "test"}

	if err := vs.Upsert(ctx, "mykey", vec, meta); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Vec file should exist.
	vecPath := filepath.Join(dir, "vectors", "mykey.vec")
	if _, err := os.Stat(vecPath); err != nil {
		t.Fatalf("mykey.vec not written: %v", err)
	}

	// Meta file should exist and be valid JSON.
	metaPath := filepath.Join(dir, "vectors", "mykey.meta")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("mykey.meta not written: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("meta JSON invalid: %v", err)
	}

	// Cache hit: Search should return the upserted entry.
	results, err := vs.Search(ctx, vec, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Metadata["src"] != "test" {
		t.Errorf("expected src=test, got %q", results[0].Metadata["src"])
	}
}

// TestUpsert_OverwritesExistingKey verifies second Upsert for same key replaces the entry.
func TestUpsert_OverwritesExistingKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	if err := vs.Upsert(ctx, "k", makeVec(3, 1, 0, 0), map[string]string{"v": "1"}); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if err := vs.Upsert(ctx, "k", makeVec(3, 0, 1, 0), map[string]string{"v": "2"}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	results, _ := vs.Search(ctx, makeVec(3, 0, 1, 0), 5)
	if len(results) == 0 {
		t.Fatal("expected search result")
	}
	if results[0].Metadata["v"] != "2" {
		t.Errorf("expected v=2 after overwrite, got %q", results[0].Metadata["v"])
	}
}

// TestUpsert_RejectsPathTraversal verifies that keys starting with "." or containing ".." are rejected.
func TestUpsert_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	badKeys := []string{".hidden", "../escape", "foo/../bar", "a/../../etc"}
	for _, k := range badKeys {
		err := vs.Upsert(ctx, k, makeVec(3, 1, 0, 0), nil)
		if err == nil {
			t.Errorf("expected error for key %q, got nil", k)
		}
	}
}

// TestSearch_EmptyCache returns empty slice, nil.
func TestSearch_EmptyCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)

	results, err := vs.Search(context.Background(), makeVec(3, 1, 0, 0), 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

// TestSearch_ZeroQueryVector returns empty results without panicking.
func TestSearch_ZeroQueryVector(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	if err := vs.Upsert(ctx, "a", makeVec(3, 1, 0, 0), nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := vs.Search(ctx, makeVec(3, 0, 0, 0), 5)
	if err != nil {
		t.Fatalf("Search with zero vector: %v", err)
	}
	// Zero query vector: all scores should be 0.
	for _, r := range results {
		if r.Score != 0 {
			t.Errorf("expected score=0 for zero query, got %f for key %q", r.Score, r.Key)
		}
	}
}

// TestSearch_RankedByScore verifies that results are sorted descending by cosine similarity.
func TestSearch_RankedByScore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	// a is parallel to query (score ~1.0), b is perpendicular (score ~0.0).
	query := makeVec(3, 1, 0, 0)
	if err := vs.Upsert(ctx, "a", makeVec(3, 1, 0, 0), nil); err != nil {
		t.Fatalf("Upsert a: %v", err)
	}
	if err := vs.Upsert(ctx, "b", makeVec(3, 0, 1, 0), nil); err != nil {
		t.Fatalf("Upsert b: %v", err)
	}

	results, err := vs.Search(ctx, query, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Key != "a" {
		t.Errorf("expected top result key=a, got %q (score=%f)", results[0].Key, results[0].Score)
	}
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted descending: [0].Score=%f [1].Score=%f", results[0].Score, results[1].Score)
	}
}

// TestSearch_LimitRespected verifies that Search returns at most limit results.
func TestSearch_LimitRespected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	for i := range 10 {
		v := makeVec(3, float32(i+1), 0, 0)
		if err := vs.Upsert(ctx, strings.Repeat("x", i+1), v, nil); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	results, err := vs.Search(ctx, makeVec(3, 1, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

// TestSearch_LimitLargerThanCache returns all entries when limit > cache size.
func TestSearch_LimitLargerThanCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	for i := range 4 {
		v := makeVec(3, float32(i+1), 0, 0)
		if err := vs.Upsert(ctx, strings.Repeat("k", i+1), v, nil); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	results, err := vs.Search(ctx, makeVec(3, 1, 0, 0), 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 results, got %d", len(results))
	}
}

// TestSearch_CosineSimilarityCorrect verifies the score values are cosine similarities.
func TestSearch_CosineSimilarityCorrect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	v := makeVec(4, 1.0, 2.0, 3.0, 4.0)
	q := makeVec(4, 4.0, 3.0, 2.0, 1.0)
	if err := vs.Upsert(ctx, "v", v, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := vs.Search(ctx, q, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := cosineSim(q, v)
	if math.Abs(float64(results[0].Score-expected)) > 1e-5 {
		t.Errorf("cosine score mismatch: got %f, expected %f", results[0].Score, expected)
	}
}

// TestUpsert_DiskPersistedAcrossInstances verifies that data is durable across New() calls.
func TestUpsert_DiskPersistedAcrossInstances(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()

	vs1, _ := vector.New(dir)
	if err := vs1.Upsert(ctx, "persist", makeVec(3, 1, 0, 0), map[string]string{"durable": "yes"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	vs2, err := vector.New(dir)
	if err != nil {
		t.Fatalf("New second instance: %v", err)
	}

	results, err := vs2.Search(ctx, makeVec(3, 1, 0, 0), 5)
	if err != nil {
		t.Fatalf("Search on second instance: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result on second instance, got %d", len(results))
	}
	if results[0].Metadata["durable"] != "yes" {
		t.Errorf("expected durable=yes, got %q", results[0].Metadata["durable"])
	}
}

// TestUpsert_ReadOnlyDir verifies Upsert returns an error when the directory is read-only.
func TestUpsert_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	// Make the vectors dir read-only so CreateTemp fails.
	if err := os.Chmod(filepath.Join(dir, "vectors"), 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "vectors"), 0700) })

	err := vs.Upsert(ctx, "k", makeVec(3, 1, 0, 0), nil)
	if err == nil {
		t.Fatal("expected error for read-only dir, got nil")
	}
}

// TestNew_CorruptVecFile verifies New returns an error if a .vec file has bad content.
func TestNew_CorruptVecFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "bot")
	if err := os.MkdirAll(filepath.Join(base, "vectors"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write a .vec file with only a dim header but no data.
	vecPath := filepath.Join(base, "vectors", "broken.vec")
	f, _ := os.Create(vecPath)
	// Write dim=1000 but no float data.
	_ = binary.Write(f, binary.LittleEndian, uint32(1000))
	_ = f.Close()

	_, err := vector.New(base)
	if err == nil {
		t.Fatal("expected error loading corrupt .vec file, got nil")
	}
}

// TestNew_CorruptMetaFile verifies New returns an error if a .meta file has bad JSON.
func TestNew_CorruptMetaFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "bot")
	if err := os.MkdirAll(filepath.Join(base, "vectors"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write a valid .vec file.
	vec := []float32{1.0}
	vecPath := filepath.Join(base, "vectors", "corrupt_meta.vec")
	f, _ := os.Create(vecPath)
	_ = binary.Write(f, binary.LittleEndian, uint32(len(vec)))
	_ = binary.Write(f, binary.LittleEndian, vec)
	_ = f.Close()

	// Write invalid JSON to the .meta file.
	metaPath := filepath.Join(base, "vectors", "corrupt_meta.meta")
	if err := os.WriteFile(metaPath, []byte("not-json"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := vector.New(base)
	if err == nil {
		t.Fatal("expected error loading corrupt .meta file, got nil")
	}
}

// TestNew_LoadsVecWithoutMeta verifies New tolerates a missing .meta file (uses stem as key).
func TestNew_LoadsVecWithoutMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "bot")
	if err := os.MkdirAll(filepath.Join(base, "vectors"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write a .vec file with no corresponding .meta file.
	vec := []float32{1.0, 0.0}
	vecPath := filepath.Join(base, "vectors", "nometa.vec")
	f, _ := os.Create(vecPath)
	_ = binary.Write(f, binary.LittleEndian, uint32(len(vec)))
	_ = binary.Write(f, binary.LittleEndian, vec)
	_ = f.Close()

	vs, err := vector.New(base)
	if err != nil {
		t.Fatalf("New with missing .meta: %v", err)
	}
	results, _ := vs.Search(context.Background(), []float32{1.0, 0.0}, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Key should fall back to stem name.
	if results[0].Key != "nometa" {
		t.Errorf("expected key=nometa, got %q", results[0].Key)
	}
}

// TestBulkCache_WithKeys verifies BulkCache accepts an explicit keys slice.
func TestBulkCache_WithKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)

	vecs := [][]float32{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
	}
	keys := []string{"alpha", "beta"}
	if err := vs.BulkCache(vecs, keys); err != nil {
		t.Fatalf("BulkCache: %v", err)
	}

	results, err := vs.Search(context.Background(), []float32{1.0, 0.0, 0.0}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Key != "alpha" {
		t.Errorf("expected top result key=alpha, got %q", results[0].Key)
	}
}

// TestBulkCache_OverwritesExistingKey verifies BulkCache replaces an existing entry.
func TestBulkCache_OverwritesExistingKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vs, _ := vector.New(dir)
	ctx := context.Background()

	_ = vs.Upsert(ctx, "x", []float32{1, 0, 0}, nil)
	// Overwrite x via BulkCache pointing in a different direction.
	_ = vs.BulkCache([][]float32{{0, 1, 0}}, []string{"x"})

	results, _ := vs.Search(ctx, []float32{0, 1, 0}, 1)
	if len(results) == 0 || results[0].Key != "x" {
		t.Fatal("expected x to be findable after BulkCache overwrite")
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 after overwrite, got %f", results[0].Score)
	}
}

// TestSearch100kVectors verifies Search on 100k in-memory vectors runs in < 100ms.
func TestSearch100kVectors(t *testing.T) {
	t.Parallel()
	const n = 100_000
	const dim = 512

	dir := t.TempDir()
	vs, err := vector.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build vectors in memory.
	vecs := make([][]float32, n)
	for i := range n {
		v := make([]float32, dim)
		for j := range dim {
			v[j] = float32((i+j)%1000) * 0.001
		}
		vecs[i] = v
	}

	// BulkCache loads all vectors into the in-memory cache without disk I/O,
	// allowing us to measure pure search throughput.
	if err := vs.BulkCache(vecs, nil); err != nil {
		t.Fatalf("BulkCache: %v", err)
	}

	query := make([]float32, dim)
	for i := range dim {
		query[i] = float32(i%1000) * 0.001
	}

	start := time.Now()
	results, err := vs.Search(context.Background(), query, 10)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}
	if elapsed >= searchTimeBudget {
		t.Errorf("Search over 100k vectors took %v, expected < %v (budget includes race-detector overhead when applicable)",
			elapsed, searchTimeBudget)
	}
	t.Logf("Search over %d vectors of dim %d: %v (budget: %v)", n, dim, elapsed, searchTimeBudget)
}
