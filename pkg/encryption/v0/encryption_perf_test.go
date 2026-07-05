//go:build perf

package encryption

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// perfSmallPayload sizes the typical secret-value string an encrypt hook
// touches on a threeport object PUT: a database password, an OAuth
// client secret, an API token.
const perfSmallPayload = 64

// perfLargePayload sizes the outlier: a base64-wrapped certificate bundle
// or a long environment variable value. Anything above this typically
// belongs in a secret store; the ceiling covers the pathological path.
const perfLargePayload = 8 * 1024

// perfCryptIterations sizes the sample count for the crypt threshold
// tests. Enough samples to smooth per-iteration variance while keeping
// the perf suite fast.
const perfCryptIterations = 200

// perfEncryptThreshold caps the p95 encrypt cost for a perfSmallPayload
// payload. AES-GCM on a hot goroutine sits well under a millisecond;
// the ceiling here allows headroom for CI variance without letting a
// regression cross into user-visible territory on a PUT hot path.
const perfEncryptThreshold = 5 * time.Millisecond

// perfDecryptThreshold caps the p95 decrypt cost for a perfSmallPayload
// payload. The decrypt path runs on every GET that projects an encrypted
// field, so a regression here surfaces as a broad read-path slowdown.
const perfDecryptThreshold = 5 * time.Millisecond

// perfCryptFixture holds a pre-generated key and a pre-encrypted small
// payload so the bench and threshold tests can share setup without
// paying the key-generation cost on every iteration.
type perfCryptFixture struct {
	key        string
	plaintext  string
	ciphertext string
}

// newPerfCryptFixture returns a fixture with a fresh key and a payload
// pre-encrypted for the decrypt paths. Fails the caller if key
// generation or the initial encrypt errors.
func newPerfCryptFixture(tb testing.TB, size int) perfCryptFixture {
	tb.Helper()
	key, err := GenerateKey()
	if err != nil {
		tb.Fatalf("failed to generate key: %v", err)
	}
	plaintext := strings.Repeat("a", size)
	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		tb.Fatalf("failed to prime ciphertext: %v", err)
	}
	return perfCryptFixture{key: key, plaintext: plaintext, ciphertext: ciphertext}
}

// perfCryptPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfCryptPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportCryptPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportCryptPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfCryptPercentileIndex(len(durations), 50)]
	p95 := durations[perfCryptPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}

// BenchmarkEncryptSmall measures Encrypt throughput on a perfSmallPayload
// payload. Reports p50 and p95 so CI can trend a regression before it
// crosses the TestEncryptSmallThresholdUs ceiling.
func BenchmarkEncryptSmall(b *testing.B) {
	fx := newPerfCryptFixture(b, perfSmallPayload)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := Encrypt(fx.key, fx.plaintext); err != nil {
			b.Fatalf("encrypt failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportCryptPercentiles(b, durations)
}

// BenchmarkDecryptSmall measures Decrypt throughput on a perfSmallPayload
// payload. Peers with BenchmarkEncryptSmall so a regression in either
// direction surfaces symmetrically.
func BenchmarkDecryptSmall(b *testing.B) {
	fx := newPerfCryptFixture(b, perfSmallPayload)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := Decrypt(fx.key, fx.ciphertext); err != nil {
			b.Fatalf("decrypt failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportCryptPercentiles(b, durations)
}

// BenchmarkEncryptLarge measures Encrypt throughput on a perfLargePayload
// payload. Isolates the byte-throughput portion from the fixed-cost
// setup so a regression in the AES-GCM inner loop is visible separately
// from a regression in the setup path.
func BenchmarkEncryptLarge(b *testing.B) {
	fx := newPerfCryptFixture(b, perfLargePayload)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := Encrypt(fx.key, fx.plaintext); err != nil {
			b.Fatalf("encrypt failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportCryptPercentiles(b, durations)
}

// BenchmarkDecryptLarge measures Decrypt throughput on a perfLargePayload
// payload; peer of BenchmarkEncryptLarge.
func BenchmarkDecryptLarge(b *testing.B) {
	fx := newPerfCryptFixture(b, perfLargePayload)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := Decrypt(fx.key, fx.ciphertext); err != nil {
			b.Fatalf("decrypt failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportCryptPercentiles(b, durations)
}

// TestEncryptSmallThresholdUs asserts the encrypt p95 over
// perfCryptIterations runs stays under perfEncryptThreshold for a
// perfSmallPayload payload. Guards a regression on the PUT hot path
// where every encrypted-field write pays this cost.
func TestEncryptSmallThresholdUs(t *testing.T) {
	fx := newPerfCryptFixture(t, perfSmallPayload)

	// warm-up: prime the cipher construction path so the first sample
	// doesn't dominate the p95
	if _, err := Encrypt(fx.key, fx.plaintext); err != nil {
		t.Fatalf("warm-up encrypt failed: %v", err)
	}

	// timed runs: collect perfCryptIterations samples for the p95 read
	samples := make([]time.Duration, perfCryptIterations)
	for i := range samples {
		start := time.Now()
		if _, err := Encrypt(fx.key, fx.plaintext); err != nil {
			t.Fatalf("encrypt iteration %d failed: %v", i, err)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfCryptPercentileIndex(len(samples), 95)]

	if p95 > perfEncryptThreshold {
		t.Fatalf(
			"encrypt p95 %s exceeds threshold %s (%d-byte payload); "+
				"check for regression in cipher construction or GCM seal",
			p95, perfEncryptThreshold, perfSmallPayload,
		)
	}
}

// TestDecryptSmallThresholdUs asserts the decrypt p95 over
// perfCryptIterations runs stays under perfDecryptThreshold for a
// perfSmallPayload payload. Guards a regression on the GET path where
// every encrypted-field read pays this cost.
func TestDecryptSmallThresholdUs(t *testing.T) {
	fx := newPerfCryptFixture(t, perfSmallPayload)

	// warm-up: prime the cipher construction path so the first sample
	// doesn't dominate the p95
	if _, err := Decrypt(fx.key, fx.ciphertext); err != nil {
		t.Fatalf("warm-up decrypt failed: %v", err)
	}

	// timed runs: collect perfCryptIterations samples for the p95 read
	samples := make([]time.Duration, perfCryptIterations)
	for i := range samples {
		start := time.Now()
		if _, err := Decrypt(fx.key, fx.ciphertext); err != nil {
			t.Fatalf("decrypt iteration %d failed: %v", i, err)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfCryptPercentileIndex(len(samples), 95)]

	if p95 > perfDecryptThreshold {
		t.Fatalf(
			"decrypt p95 %s exceeds threshold %s (%d-byte payload); "+
				"check for regression in cipher construction or GCM open",
			p95, perfDecryptThreshold, perfSmallPayload,
		)
	}
}
