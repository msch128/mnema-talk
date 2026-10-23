package auth

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

// ErrServerBusy answers an unauthenticated password check that could not get a
// hashing slot in time. It is not a verdict on the credentials.
var ErrServerBusy = httpx.ErrUnavailable("the server is busy, try again shortly")

// bcryptSlots bounds how many password hashes run at once across login,
// register, reauth and password changes, so a flood of attempts cannot starve
// the CPU of everything else.
var bcryptSlots = make(chan struct{}, max(2, runtime.NumCPU()))

var (
	// bcryptWaiting counts requests queued for a slot.
	bcryptWaiting atomic.Int32
	// bcryptMaxQueue is how many may queue; more are refused at once.
	bcryptMaxQueue = int32(4 * cap(bcryptSlots))
	// bcryptMaxWait is how long a queued request waits before giving up.
	bcryptMaxWait = 2 * time.Second
)

// acquireBcrypt blocks until a slot is free. It is for callers that already
// passed authentication and their own per-user limits.
func acquireBcrypt() func() {
	bcryptSlots <- struct{}{}
	return func() { <-bcryptSlots }
}

// acquireBcryptCtx is the variant for unauthenticated work: it gives up when
// ctx ends, after bcryptMaxWait, or at once when the queue is already full, so
// a flood of attempts cannot pile up goroutines behind the CPU.
func acquireBcryptCtx(ctx context.Context) (func(), error) {
	release := func() { <-bcryptSlots }
	select {
	case bcryptSlots <- struct{}{}:
		return release, nil
	default:
	}
	if bcryptWaiting.Add(1) > bcryptMaxQueue {
		bcryptWaiting.Add(-1)
		return nil, ErrServerBusy
	}
	defer bcryptWaiting.Add(-1)
	timer := time.NewTimer(bcryptMaxWait)
	defer timer.Stop()
	select {
	case bcryptSlots <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrServerBusy
	}
}

// burnPasswordCheck spends the same bcrypt work as checking a real password.
func burnPasswordCheck(ctx context.Context, password string) error {
	release, err := acquireBcryptCtx(ctx)
	if err != nil {
		return err
	}
	defer release()
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	return nil
}

// checkPasswordCtx is CheckPassword with a bounded wait for a hashing slot.
func checkPasswordCtx(ctx context.Context, password, hash string) (bool, error) {
	release, err := acquireBcryptCtx(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil, nil
}

// hashPasswordCtx is HashPassword with a bounded wait for a hashing slot.
func hashPasswordCtx(ctx context.Context, password string) (string, error) {
	release, err := acquireBcryptCtx(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	b, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	return string(b), err
}

// HashPassword hashes password with bcrypt, waiting for a free slot.
func HashPassword(password string) (string, error) {
	defer acquireBcrypt()()
	b, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	return string(b), err
}

// CheckPassword reports whether password matches hash, waiting for a free slot.
func CheckPassword(password, hash string) bool {
	defer acquireBcrypt()()
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
