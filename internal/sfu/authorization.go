package sfu

import (
	"errors"
	"io"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// authorizeMedia is immutable after peer construction. Native connections use
// an owner-controlled lease latch; browser peers retain the nil/default path.
func (p *Peer) mediaAuthorized() bool { return p.authorizeMedia == nil || p.authorizeMedia() }

// authorizedTrack binds the shared publisher track to a recipient-specific
// writer through Pion's public TrackLocal interfaces. This initial-write guard
// complements the innermost interceptor below, which also gates cached RTX. Payloads
// pass through unchanged; this supplies authorization, not frame encryption.
type authorizedTrack struct {
	webrtc.TrackLocal
	authorize func() bool
	source    func() bool
	sources   *sourceAuthorizationRegistry
	bindings  sync.Map
}

func (t *authorizedTrack) Bind(ctx webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	var entry *sourceAuthorizationBinding
	if t.source != nil {
		if t.sources == nil {
			return webrtc.RTPCodecParameters{}, errors.New("native source authorization unavailable")
		}
		entry = t.sources.bind(ctx.ID(), t.source)
		t.bindings.Store(ctx.ID(), entry)
	}
	codec, err := t.TrackLocal.Bind(authorizedTrackContext{TrackLocalContext: ctx, writer: authorizedRTPWriter{writer: ctx.WriteStream(), authorize: t.authorize}})
	if err != nil && entry != nil {
		t.sources.remove(ctx.ID(), entry)
		t.bindings.Delete(ctx.ID())
	}
	return codec, err
}

func (t *authorizedTrack) Unbind(ctx webrtc.TrackLocalContext) error {
	if entry, ok := t.bindings.LoadAndDelete(ctx.ID()); ok {
		t.sources.remove(ctx.ID(), entry.(*sourceAuthorizationBinding))
	}
	return t.TrackLocal.Unbind(ctx)
}

// Source registrations exist only between TrackLocal.Bind and interceptor
// BindLocalStream. The final writer captures the immutable callback before the
// default cache is constructed, so unbind/deletion cannot make a native source
// fail open and no registry lock is taken on a packet write.
type sourceAuthorizationBinding struct{ authorize func() bool }
type sourceAuthorizationRegistry struct {
	mu      sync.Mutex
	pending map[string]*sourceAuthorizationBinding
}

func (r *sourceAuthorizationRegistry) bind(id string, authorize func() bool) *sourceAuthorizationBinding {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = make(map[string]*sourceAuthorizationBinding)
	}
	entry := &sourceAuthorizationBinding{authorize: authorize}
	r.pending[id] = entry
	return entry
}
func (r *sourceAuthorizationRegistry) capture(id string) *sourceAuthorizationBinding {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.pending[id]
	delete(r.pending, id)
	return entry
}
func (r *sourceAuthorizationRegistry) remove(id string, expected *sourceAuthorizationBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[id] == expected {
		delete(r.pending, id)
	}
}

type authorizedTrackContext struct {
	webrtc.TrackLocalContext
	writer webrtc.TrackLocalWriter
}

func (c authorizedTrackContext) WriteStream() webrtc.TrackLocalWriter { return c.writer }

type authorizedRTPWriter struct {
	writer    webrtc.TrackLocalWriter
	authorize func() bool
}

func (w authorizedRTPWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	if !w.authorize() {
		return 0, io.ErrClosedPipe
	}
	return w.writer.WriteRTP(header, payload)
}
func (w authorizedRTPWriter) Write(raw []byte) (int, error) {
	if !w.authorize() {
		return 0, io.ErrClosedPipe
	}
	return w.writer.Write(raw)
}

// mediaAuthorizationFactory precedes Pion's default interceptors. Every native
// source or recipient RTP write, including cached NACK retransmissions/RTX,
// checks its captured lease latch. Ordinary browser bindings return the original
// writer, preserving their behavior and avoiding any per-packet registry access.
type mediaAuthorizationFactory struct {
	authorize func() bool
	sources   *sourceAuthorizationRegistry
}

func (f mediaAuthorizationFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &mediaAuthorizationInterceptor{authorize: f.authorize, sources: f.sources}, nil
}

type mediaAuthorizationInterceptor struct {
	interceptor.NoOp
	authorize func() bool
	sources   *sourceAuthorizationRegistry
}

func (i *mediaAuthorizationInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	source := i.sources.capture(info.ID)
	if i.authorize == nil && source == nil {
		return writer // deliberate ordinary browser source and recipient.
	}
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		if (i.authorize != nil && !i.authorize()) || (source != nil && (source.authorize == nil || !source.authorize())) {
			return 0, io.ErrClosedPipe
		}
		return writer.Write(header, payload, attributes)
	})
}
