package lt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Sample is one scrape of the server's /api/metrics. Values holds the series
// of the names Interesting keeps, summed over their labels.
type Sample struct {
	At     time.Time          `json:"at"`
	Phase  string             `json:"phase"`
	Values map[string]float64 `json:"values"`
}

// Interesting says whether a metric name is kept in a Sample.
func Interesting(name string) bool {
	switch name {
	case "go_goroutines", "go_memstats_alloc_bytes", "go_memstats_sys_bytes", "go_memstats_heap_inuse_bytes":
		return true
	}
	return strings.HasPrefix(name, "process_") || strings.HasPrefix(name, "mnema_")
}

// ParseMetrics reads the Prometheus text format. Labelled series of one name
// are added up; histogram buckets are skipped.
func ParseMetrics(r io.Reader) map[string]float64 {
	out := map[string]float64{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		name, value, ok := splitSeries(line)
		if !ok || !Interesting(name) || strings.HasSuffix(name, "_bucket") {
			continue
		}
		out[name] += value
	}
	return out
}

// splitSeries splits `name{labels} value [timestamp]`.
func splitSeries(line string) (string, float64, bool) {
	end := strings.IndexAny(line, "{ ")
	if end < 0 {
		return "", 0, false
	}
	name := line[:end]
	rest := line[end:]
	if rest[0] == '{' {
		close := strings.LastIndexByte(rest, '}')
		if close < 0 {
			return "", 0, false
		}
		rest = rest[close+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", 0, false
	}
	return name, v, true
}

// Scraper reads /api/metrics with the METRICS_TOKEN.
type Scraper struct {
	URL    string
	Token  string
	Client *http.Client
}

// NewScraper returns nil when no token is given: the server has no metrics
// endpoint then, and the runs report that the server was not measured.
func NewScraper(base *url.URL, token string) *Scraper {
	if token == "" {
		return nil
	}
	return &Scraper{URL: base.String() + "/api/metrics", Token: token, Client: &http.Client{Timeout: 8 * time.Second}}
}

// Scrape fetches one sample.
func (s *Scraper) Scrape(ctx context.Context, phase string) (Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return Sample{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := s.Client.Do(req)
	if err != nil {
		return Sample{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The status only: the body and the request never reach a log.
		return Sample{}, fmt.Errorf("metrics: %s", resp.Status)
	}
	return Sample{At: time.Now(), Phase: phase, Values: ParseMetrics(resp.Body)}, nil
}

// Sampler scrapes in the background every interval and keeps the samples,
// labelled with the current phase.
type Sampler struct {
	scraper  *Scraper
	interval time.Duration
	phase    atomic.Value // string

	mu      sync.Mutex
	samples []Sample
	errs    int
	stop    context.CancelFunc
	done    chan struct{}
}

// StartSampler begins sampling; a nil scraper gives a sampler that records
// nothing. Phase starts as "pre" and the first sample is taken at once.
func StartSampler(sc *Scraper, interval time.Duration) *Sampler {
	s := &Sampler{scraper: sc, interval: interval, done: make(chan struct{})}
	s.phase.Store("pre")
	if sc == nil {
		close(s.done)
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	go s.loop(ctx)
	return s
}

func (s *Sampler) loop(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		s.sampleNow(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Sampler) sampleNow(ctx context.Context) (Sample, bool) {
	smp, err := s.scraper.Scrape(ctx, s.phase.Load().(string))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if ctx.Err() == nil {
			s.errs++
		}
		return Sample{}, false
	}
	s.samples = append(s.samples, smp)
	return smp, true
}

// SetPhase labels the samples that follow.
func (s *Sampler) SetPhase(p string) { s.phase.Store(p) }

// Mark takes a sample right now, for the edges of a phase, and returns it
// (false when the server is not measured or did not answer).
func (s *Sampler) Mark(phase string) (Sample, bool) {
	s.SetPhase(phase)
	if s.scraper == nil {
		return Sample{}, false
	}
	return s.sampleNow(context.Background())
}

// Stop ends the sampling and returns the samples and the number of failed scrapes.
func (s *Sampler) Stop() ([]Sample, int) {
	if s.stop != nil {
		s.stop()
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...), s.errs
}

// Enabled tells whether the server is being measured.
func (s *Sampler) Enabled() bool { return s.scraper != nil }
