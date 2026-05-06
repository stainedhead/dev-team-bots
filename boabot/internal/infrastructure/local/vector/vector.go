// Package vector provides a local on-disk implementation of domain.VectorStore.
// It is intended for single-binary operation without any cloud infrastructure.
//
// # On-disk format
//
// Each key is stored as two files under <basePath>/vectors/:
//   - <key>.vec  — binary, little-endian: [uint32 dim][float32 * dim]
//   - <key>.meta — JSON: {"key":"...", "metadata":{"k":"v",...}}
//
// Both files are written atomically (temp file + os.Rename). On startup, all
// existing .vec/.meta pairs are loaded into an in-memory cache. Subsequent
// searches operate entirely from the cache, so the Search path holds only a
// read lock and incurs no disk I/O.
//
// # Performance
//
// The in-memory cache stores entries as a flat slice for cache-friendly
// sequential access. Each cachedEntry pre-computes and stores the vector
// magnitude so the Search hot path only needs dot products. The dot product
// loop is written in plain Go so the compiler can auto-vectorise it.
//
// # Path safety
//
// Keys may contain forward slashes (treated as subdirectory separators) but
// must not start with "." or contain ".." segments. This prevents path-traversal
// attacks against the vectors/ directory.
package vector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// cachedEntry holds an in-memory vector with its pre-computed magnitude and metadata.
type cachedEntry struct {
	key      string
	vector   []float32
	mag      float64 // pre-computed L2 magnitude of vector
	metadata map[string]string
}

// metaFile is the JSON structure persisted to <key>.meta.
type metaFile struct {
	Key      string            `json:"key"`
	Metadata map[string]string `json:"metadata"`
}

// VectorStore implements domain.VectorStore using the local filesystem.
type VectorStore struct {
	basePath   string // <memory.path>/<bot-name>
	vectorsDir string // basePath/vectors
	mu         sync.RWMutex
	index      map[string]int // key → position in entries slice
	entries    []*cachedEntry // flat slice for cache-friendly search
}

// New constructs a VectorStore rooted at basePath.
// It calls os.MkdirAll on <basePath>/vectors and then loads all existing
// .vec/.meta pairs from disk into the in-memory cache.
// Returns an error if the directory cannot be created or if any file fails to parse.
func New(basePath string) (*VectorStore, error) {
	vectorsDir := filepath.Join(basePath, "vectors")
	if err := os.MkdirAll(vectorsDir, 0700); err != nil {
		return nil, fmt.Errorf("local/vector: create vectors dir %q: %w", vectorsDir, err)
	}

	vs := &VectorStore{
		basePath:   basePath,
		vectorsDir: vectorsDir,
		index:      make(map[string]int),
	}

	if err := vs.loadAll(); err != nil {
		return nil, fmt.Errorf("local/vector: load existing vectors: %w", err)
	}
	return vs, nil
}

// Upsert stores the vector and metadata for key, both on disk and in the
// in-memory cache. Both files are written atomically using temp-file + rename.
//
// Key safety: returns an error if the key starts with "." or if any segment
// of the key is "..".
func (vs *VectorStore) Upsert(_ context.Context, key string, vector []float32, metadata map[string]string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	if err := vs.writeVec(key, vector); err != nil {
		return err
	}
	if err := vs.writeMeta(key, metadata); err != nil {
		return err
	}

	// Copy inputs to avoid aliasing.
	metaCopy := make(map[string]string, len(metadata))
	for k, v := range metadata {
		metaCopy[k] = v
	}
	vecCopy := make([]float32, len(vector))
	copy(vecCopy, vector)

	e := &cachedEntry{
		key:      key,
		vector:   vecCopy,
		mag:      magnitude(vecCopy),
		metadata: metaCopy,
	}

	vs.mu.Lock()
	if pos, ok := vs.index[key]; ok {
		// Overwrite in place.
		vs.entries[pos] = e
	} else {
		vs.index[key] = len(vs.entries)
		vs.entries = append(vs.entries, e)
	}
	vs.mu.Unlock()
	return nil
}

// Search computes cosine similarity between query and every cached vector,
// then returns the top-limit results sorted by descending score.
//
// If the cache is empty, it returns an empty slice and nil error.
// If query is the zero vector, all scores are 0.0 and an empty slice is returned.
func (vs *VectorStore) Search(_ context.Context, query []float32, limit int) ([]domain.VectorResult, error) {
	queryMag := magnitude(query)

	vs.mu.RLock()
	entries := vs.entries
	vs.mu.RUnlock()

	if len(entries) == 0 {
		return []domain.VectorResult{}, nil
	}

	// Zero query vector: return empty results (all cosines would be 0).
	if queryMag == 0 {
		return []domain.VectorResult{}, nil
	}

	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, len(entries))
	for i, e := range entries {
		score := float32(0)
		if e.mag > 0 {
			score = float32(dotProduct(query, e.vector) / (queryMag * e.mag))
		}
		scores[i] = scored{idx: i, score: score}
	}

	// Partial sort: we only need the top-limit items.
	// For limit << len(entries) a selection sort is O(n*limit); for large limit
	// a full sort is O(n log n). We use full sort here for simplicity; it is
	// fast enough for 100k entries on modern hardware.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if limit > len(scores) {
		limit = len(scores)
	}

	results := make([]domain.VectorResult, limit)
	for i := range limit {
		e := entries[scores[i].idx]
		metaCopy := make(map[string]string, len(e.metadata))
		for k, v := range e.metadata {
			metaCopy[k] = v
		}
		results[i] = domain.VectorResult{
			Key:      e.key,
			Score:    scores[i].score,
			Metadata: metaCopy,
		}
	}
	return results, nil
}

// BulkCache inserts a slice of vectors directly into the in-memory cache
// without performing any disk I/O. Keys are auto-generated as "__bulk_N"
// where N is the index. The keys slice may be nil, in which case auto-generated
// keys are used for all entries.
//
// This method is intended for performance testing of the Search path
// independently of disk I/O. It must not be called concurrently with Upsert.
func (vs *VectorStore) BulkCache(vecs [][]float32, keys []string) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Pre-allocate to minimise slice growth.
	if cap(vs.entries)-len(vs.entries) < len(vecs) {
		grown := make([]*cachedEntry, len(vs.entries), len(vs.entries)+len(vecs))
		copy(grown, vs.entries)
		vs.entries = grown
	}

	for i, v := range vecs {
		key := fmt.Sprintf("__bulk_%d", i)
		if keys != nil && i < len(keys) {
			key = keys[i]
		}
		cp := make([]float32, len(v))
		copy(cp, v)
		e := &cachedEntry{key: key, vector: cp, mag: magnitude(cp)}
		if pos, ok := vs.index[key]; ok {
			vs.entries[pos] = e
		} else {
			vs.index[key] = len(vs.entries)
			vs.entries = append(vs.entries, e)
		}
	}
	return nil
}

// --- internal helpers ---------------------------------------------------------

// validateKey returns an error if the key is unsafe for filesystem use.
func validateKey(key string) error {
	if strings.HasPrefix(key, ".") {
		return fmt.Errorf("local/vector: key %q must not start with '.'", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return fmt.Errorf("local/vector: key %q contains '..' segment", key)
		}
	}
	return nil
}

// keyToPath converts a key to a filesystem-safe path prefix (no extension).
func (vs *VectorStore) keyToPath(key string) string {
	return filepath.Join(vs.vectorsDir, filepath.FromSlash(key))
}

// writeVec writes the binary vector file for key atomically.
func (vs *VectorStore) writeVec(key string, vector []float32) error {
	dest := vs.keyToPath(key) + ".vec"
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return fmt.Errorf("local/vector: create parent dir for %q: %w", key, err)
	}
	return atomicWrite(dest, encodeVec(vector))
}

// writeMeta writes the JSON metadata file for key atomically.
func (vs *VectorStore) writeMeta(key string, metadata map[string]string) error {
	dest := vs.keyToPath(key) + ".meta"
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return fmt.Errorf("local/vector: create parent dir for meta %q: %w", key, err)
	}
	mf := metaFile{Key: key, Metadata: metadata}
	data, err := json.Marshal(mf)
	if err != nil {
		return fmt.Errorf("local/vector: marshal meta for %q: %w", key, err)
	}
	return atomicWrite(dest, data)
}

// atomicWrite writes data to dest using a temp file + rename for atomicity.
// The temp file is created in the same directory as dest so the rename is
// always within the same filesystem (avoiding cross-device link errors).
func atomicWrite(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".tmp-vec-*")
	if err != nil {
		return fmt.Errorf("local/vector: create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close() // close before WriteFile re-opens

	if err := os.WriteFile(tmpName, data, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local/vector: write temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local/vector: rename %q → %q: %w", tmpName, dest, err)
	}
	return nil
}

// loadAll scans vectorsDir for .vec files and loads each paired .vec/.meta
// into the in-memory cache. Missing .meta files are tolerated (empty metadata).
func (vs *VectorStore) loadAll() error {
	dirEntries, err := os.ReadDir(vs.vectorsDir)
	if err != nil {
		return fmt.Errorf("read vectors dir: %w", err)
	}

	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".vec") {
			continue
		}
		stem := strings.TrimSuffix(de.Name(), ".vec")
		if err := vs.loadEntry(stem); err != nil {
			return err
		}
	}
	return nil
}

// loadEntry loads a single .vec/.meta pair identified by stem (filename without extension).
func (vs *VectorStore) loadEntry(stem string) error {
	vecPath := filepath.Join(vs.vectorsDir, stem+".vec")
	f, err := os.Open(vecPath)
	if err != nil {
		return fmt.Errorf("open %s.vec: %w", stem, err)
	}
	defer func() { _ = f.Close() }()

	var dim uint32
	if err := binary.Read(f, binary.LittleEndian, &dim); err != nil {
		return fmt.Errorf("read dim from %s.vec: %w", stem, err)
	}
	vec := make([]float32, dim)
	if err := binary.Read(f, binary.LittleEndian, vec); err != nil {
		return fmt.Errorf("read vec data from %s.vec: %w", stem, err)
	}

	// Load metadata (optional — tolerate missing .meta).
	var key string
	var metadata map[string]string
	metaPath := filepath.Join(vs.vectorsDir, stem+".meta")
	metaData, err := os.ReadFile(metaPath)
	if err == nil {
		var mf metaFile
		if err := json.Unmarshal(metaData, &mf); err != nil {
			return fmt.Errorf("parse %s.meta: %w", stem, err)
		}
		key = mf.Key
		metadata = mf.Metadata
	} else if os.IsNotExist(err) {
		key = stem
	} else {
		return fmt.Errorf("read %s.meta: %w", stem, err)
	}

	e := &cachedEntry{key: key, vector: vec, mag: magnitude(vec), metadata: metadata}
	vs.index[key] = len(vs.entries)
	vs.entries = append(vs.entries, e)
	return nil
}

// dotProduct computes the dot product of two float32 slices.
// Written as a simple loop so the compiler can auto-vectorise (SIMD).
func dotProduct(a, b []float32) float64 {
	var sum float64
	n := len(a)
	for i := 0; i < n; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// magnitude returns the L2 norm of v.
func magnitude(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// encodeVec serialises a float32 slice to the on-disk binary format:
// [uint32 dim (little-endian)][float32 * dim (little-endian)].
func encodeVec(vector []float32) []byte {
	dim := uint32(len(vector))
	// 4 bytes for dim + 4 bytes per float32.
	buf := make([]byte, 4+4*int(dim))
	binary.LittleEndian.PutUint32(buf[0:4], dim)
	for i, f := range vector {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(buf[4+4*i:], bits)
	}
	return buf
}
