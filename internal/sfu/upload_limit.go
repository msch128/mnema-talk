package sfu

import (
	"log/slog"
	"slices"
	"time"
)

// The voice upload limit (VOICE_UPLOAD_LIMIT_MBIT) keeps the media the SFU
// sends under what the connection can carry. When the egress goes above
// uploadHigh of the limit, viewers on automatic are held one layer further
// below the best their picture can use, the biggest streams first, until the
// estimated saving covers the excess; below uploadLow of the limit they are
// let back, the cheapest first. The gap between the two and the holds between
// moves keep it from flapping. Audio is never touched: the governor only
// chooses among the layers of screen shares and cameras, and a viewer who
// picked a layer, or watches a track that has none, is left alone (when that
// leaves nothing to step down, the check is counted in
// mnema_sfu_upload_unmet_total and logged).
const (
	uploadHigh   = 0.85
	uploadTarget = 0.80
	uploadLow    = 0.70
	// uploadSqueezeHold waits after a step down for the egress to show it
	// (the layer switch, the keyframe and the measurement all take seconds).
	uploadSqueezeHold = 8 * time.Second
	// uploadRelaxHold waits longer after a step up, which is the risky one.
	uploadRelaxHold = 20 * time.Second
	// uploadUnmetLog is how often "nothing left to step down" is logged.
	uploadUnmetLog = time.Minute
)

// uploadGovernor decides the steps; it holds no clock and no locks.
type uploadGovernor struct {
	// limit is in bits per second.
	limit     float64
	holdUntil time.Time
	loggedAt  time.Time
}

// step looks at the egress (bits per second) and the viewers it can move, and
// moves some. It returns how many viewers are held below their best layer.
func (g *uploadGovernor) step(now time.Time, egress float64, views []uploadView) (capped int) {
	defer func() {
		for _, v := range views {
			if v.squeeze > 0 {
				capped++
			}
		}
	}()
	if now.Before(g.holdUntil) {
		return 0
	}
	switch {
	case egress > g.limit*uploadHigh:
		g.squeezeDown(now, egress-g.limit*uploadTarget, views)
	case egress < g.limit*uploadLow:
		g.relax(now, g.limit*uploadTarget-egress, views)
	}
	return 0
}

// squeezeDown holds the biggest consumers one layer lower until the estimated
// saving covers excess.
func (g *uploadGovernor) squeezeDown(now time.Time, excess float64, views []uploadView) {
	order := slices.Clone(views)
	slices.SortStableFunc(order, func(a, b uploadView) int {
		switch {
		case a.rate > b.rate:
			return -1
		case a.rate < b.rate:
			return 1
		}
		return 0
	})
	moved := false
	for _, v := range order {
		if excess <= 0 {
			break
		}
		if v.down <= 0 {
			continue
		}
		v.vt.setSqueeze(v.squeeze + 1)
		markSqueezed(views, v.vt, v.squeeze+1)
		excess -= v.down
		moved = true
	}
	if moved {
		g.holdUntil = now.Add(uploadSqueezeHold)
		return
	}
	countUnmet()
	if now.Sub(g.loggedAt) >= uploadUnmetLog {
		g.loggedAt = now
		slog.Warn("voice upload is above VOICE_UPLOAD_LIMIT_MBIT and there is nothing left to step down: the rest is audio, tracks without layers, viewers who chose a quality, or viewers already on the smallest layer")
	}
}

// relax lets back the squeezed viewer that costs least, if the room allows.
func (g *uploadGovernor) relax(now time.Time, room float64, views []uploadView) {
	best := -1
	for i, v := range views {
		if v.squeeze > 0 && v.up <= room && (best < 0 || v.up < views[best].up) {
			best = i
		}
	}
	if best < 0 {
		return
	}
	v := views[best]
	v.vt.setSqueeze(v.squeeze - 1)
	markSqueezed(views, v.vt, v.squeeze-1)
	g.holdUntil = now.Add(uploadRelaxHold)
}

// markSqueezed records a new squeeze in the list the caller counts from.
func markSqueezed(views []uploadView, vt *viewerTrack, n int) {
	for i := range views {
		if views[i].vt == vt {
			views[i].squeeze = n
		}
	}
}
