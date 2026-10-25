//go:build integration

package s3

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"math/rand/v2"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"

	"github.com/msch128/mnema-talk/internal/config"
)

// These tests talk to a real S3-compatible server (SeaweedFS, as in the
// production compose file), because the fake in client_test.go cannot show that
// a server accepts the multipart upload an unknown-size body turns into.
// TEST_S3_ENDPOINT (with TEST_S3_ACCESS_KEY and TEST_S3_SECRET_KEY) points them
// at another running server, e.g. MinIO; otherwise a throwaway
// SeaweedFS container is started.

var (
	sfsOnce   sync.Once
	sfsClient *Client
	sfsErr    error
	sfsPool   dockertest.ClosablePool
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sfsPool != nil {
		_ = sfsPool.Close(context.Background())
	}
	os.Exit(code)
}

func seaweedClient(t *testing.T) *Client {
	t.Helper()
	sfsOnce.Do(func() { sfsClient, sfsErr = startSeaweed() })
	if sfsErr != nil {
		t.Fatalf("no S3 server: %v", sfsErr)
	}
	return sfsClient
}

func startSeaweed() (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		var err error
		if endpoint, err = runSeaweedContainer(ctx); err != nil {
			return nil, err
		}
	}
	cfg := &config.Config{S3Endpoint: endpoint, S3Region: "us-east-1", S3Bucket: "mnema-s3-test", S3AccessKey: envOr("TEST_S3_ACCESS_KEY", "test"), S3SecretKey: envOr("TEST_S3_SECRET_KEY", "test"), S3ForcePathStyle: true}
	deadline := time.Now().Add(90 * time.Second)
	for {
		c, err := New(ctx, cfg)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("wait for seaweedfs: %w", err)
		}
		time.Sleep(time.Second)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func runSeaweedContainer(ctx context.Context) (string, error) {
	pool, err := dockertest.NewPool(ctx, "", dockertest.WithMaxWait(2*time.Minute))
	if err != nil {
		return "", fmt.Errorf("dockertest pool: %w", err)
	}
	res, err := pool.Run(ctx, "chrislusf/seaweedfs",
		dockertest.WithTag("4.50"),
		dockertest.WithCmd([]string{"server", "-s3", "-s3.port=8333", "-dir=/data"}),
		dockertest.WithLabels(map[string]string{"mnema-talk.testutil": "seaweedfs"}),
		dockertest.WithoutReuse(),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.AutoRemove = true
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
		}),
	)
	if err != nil {
		_ = pool.Close(context.Background())
		return "", fmt.Errorf("run seaweedfs: %w", err)
	}
	sfsPool = pool
	return "http://" + res.GetHostPort("8333/tcp"), nil
}

// generated is a body of n pseudo-random bytes that is never held in memory.
type generated struct {
	rng  *rand.ChaCha8
	left int64
	sum  hash.Hash
}

func newGenerated(n int64) *generated {
	return &generated{rng: rand.NewChaCha8([32]byte{byte(n), byte(n >> 8), byte(n >> 16)}), left: n, sum: sha256.New()}
}

func (g *generated) Read(p []byte) (int, error) {
	if g.left == 0 {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), g.left)]
	_, _ = g.rng.Read(p)
	g.sum.Write(p)
	g.left -= int64(len(p))
	return len(p), nil
}

// peakHeap samples the heap until stop is called; stop returns how far the
// highest sample is above the heap at the start.
func peakHeap() (stop func() uint64) {
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	done, result := make(chan struct{}), make(chan uint64)
	go func() {
		var peak uint64
		for {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.HeapInuse)
			select {
			case <-done:
				result <- peak - min(peak, base.HeapInuse)
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return func() uint64 { close(done); return <-result }
}

func TestUnknownSizeUploadRoundTripsThroughRealStorage(t *testing.T) {
	c := seaweedClient(t)
	ctx := context.Background()
	for _, size := range []int64{1, partSize - 1, partSize, partSize + 1, 2 * partSize, 3*partSize + 12345} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			key := fmt.Sprintf("roundtrip/%d", size)
			body := newGenerated(size)
			if err := c.Upload(ctx, key, body, "application/zip", -1); err != nil {
				t.Fatalf("upload: %v", err)
			}
			got, err := c.GetObjectFrom(ctx, key, 0)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer got.Close()
			read := sha256.New()
			n, err := io.Copy(read, got)
			if err != nil || n != size {
				t.Fatalf("read back %d bytes, err %v; want %d", n, err, size)
			}
			if string(read.Sum(nil)) != string(body.sum.Sum(nil)) {
				t.Fatal("stored bytes differ from the uploaded ones")
			}
		})
	}
}

func TestUnknownSizeUploadHoldsOnePartInMemory(t *testing.T) {
	c := seaweedClient(t)
	const size = 160 << 20 // 32 parts
	prev := debug.SetGCPercent(50)
	defer debug.SetGCPercent(prev)

	stop := peakHeap()
	err := c.Upload(context.Background(), "memory/large", newGenerated(size), "application/zip", -1)
	grown := stop()
	t.Logf("heap grew by %d MiB while streaming %d MiB", grown>>20, size>>20)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	// One 5 MiB part is buffered; the rest is SDK and runtime overhead. Holding
	// the whole body would take over 160 MiB.
	if limit := uint64(48 << 20); grown > limit {
		t.Fatalf("heap grew by %d MiB while streaming %d MiB, want under %d MiB", grown>>20, size>>20, limit>>20)
	}
}
