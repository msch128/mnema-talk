package main

import (
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// lossFactory drops incoming RTP before the NACK generator (the factory is
// first in the chain, i.e. closest to the network), so the receiver reports
// the gaps and the SFU's retransmission path is exercised.
type lossFactory struct {
	loss   float64
	jitter time.Duration
	// kbps limits the connection's rate (0 = unlimited) with a queue of
	// queue; each connection gets a link of its own.
	kbps  int
	queue time.Duration
}

func (f *lossFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	l := &lossInterceptor{loss: f.loss, jitter: f.jitter}
	if f.kbps > 0 {
		l.link = newShaper(f.kbps, f.queue)
	}
	return l, nil
}

type lossInterceptor struct {
	interceptor.NoOp
	loss   float64
	jitter time.Duration
	link   *shaper
}

func (l *lossInterceptor) BindRemoteStream(_ *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		for {
			n, attr, err := reader.Read(b, a)
			if err != nil {
				return n, attr, err
			}
			if l.impair(n) {
				return n, attr, nil
			}
		}
	})
}

// impair applies jitter, loss and the bandwidth limit to one packet of n
// bytes and reports whether it is delivered.
func (l *lossInterceptor) impair(n int) bool {
	if l.jitter > 0 {
		time.Sleep(time.Duration(rand.Int64N(int64(l.jitter))))
	}
	if l.loss > 0 && rand.Float64() < l.loss {
		return false
	}
	if l.link == nil {
		return true
	}
	wait, ok := l.link.admit(time.Now(), n)
	if !ok {
		return false
	}
	time.Sleep(wait)
	return true
}

func newAPI(loss float64, jitter time.Duration) (*webrtc.API, error) {
	se := webrtc.SettingEngine{}
	if v := os.Getenv("ICE_INTERFACES"); v != "" {
		allowed := map[string]bool{}
		for _, n := range strings.Split(v, ",") {
			allowed[strings.TrimSpace(n)] = true
		}
		se.SetInterfaceFilter(func(name string) bool { return allowed[name] })
	}
	if os.Getenv("ICE_UDP4_ONLY") == "1" {
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	}
	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	reg := &interceptor.Registry{}
	kbps := envInt("BW_KBPS", 0)
	if loss > 0 || jitter > 0 || kbps > 0 {
		reg.Add(&lossFactory{loss: loss, jitter: jitter, kbps: kbps, queue: time.Duration(envInt("BW_QUEUE_MS", 1500)) * time.Millisecond})
	}
	if err := webrtc.RegisterDefaultInterceptors(me, reg); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(reg)), nil
}
