//go:build integration

package push

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func sub(path string, b browser) newSubscription {
	return newSubscription{endpoint: "https://push.example.com" + path, p256dh: b.p256dh(), auth: b.secret(), label: "Firefox"}
}

func TestVAPIDKeysAreCreatedOnceAndSurviveRestarts(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM push_vapid`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	keys := make([]Keys, 8)
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := LoadKeys(ctx, pool)
			if err != nil {
				t.Error(err)
			}
			keys[i] = k
		}()
	}
	wg.Wait()
	for _, k := range keys {
		if k != keys[0] || !k.valid() {
			t.Fatalf("servers starting together disagree: %+v vs %+v", k, keys[0])
		}
	}
	again, err := LoadKeys(ctx, pool)
	if err != nil || again != keys[0] {
		t.Fatalf("a restart changed the key: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM push_vapid`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("%d key rows, %v", rows, err)
	}
}

func TestMalformedStoredKeysAreAnErrorNotASilentReplacement(t *testing.T) {
	pool := testutil.DB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM push_vapid; INSERT INTO push_vapid (public_key, private_key) VALUES ('abc', 'def')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM push_vapid`) })
	if _, err := LoadKeys(ctx, pool); err == nil {
		t.Fatal("malformed keys accepted")
	}
	var public string
	if err := pool.QueryRow(ctx, `SELECT public_key FROM push_vapid`).Scan(&public); err != nil || public != "abc" {
		t.Fatalf("the stored key was replaced: %q %v", public, err)
	}
}

func TestADeviceBelongsToOneAccountAndMovesWithTheBrowser(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	ctx := context.Background()
	b := newBrowser(t)

	first, err := upsert(ctx, e.pool, alice.id, sub("/shared-browser", b))
	if err != nil || first.Label != "Firefox" || first.Fingerprint != Fingerprint("https://push.example.com/shared-browser") {
		t.Fatalf("%+v, %v", first, err)
	}
	e.exec(`UPDATE push_subscriptions SET failures = 5 WHERE id = $1`, first.ID)
	// Bob signs in on the same browser and turns push on: the device is his now.
	moved, err := upsert(ctx, e.pool, bob.id, sub("/shared-browser", b))
	if err != nil || moved.ID != first.ID {
		t.Fatalf("%+v, %v", moved, err)
	}
	if got, _ := list(ctx, e.pool, alice.id); len(got) != 1 { // her own phone from the fixture only
		t.Fatalf("alice still has %d devices", len(got))
	}
	if n := failuresOf(e, bob.id); n != 0 {
		t.Fatalf("failures %d after registering again", n)
	}
	if got, _ := list(ctx, e.pool, bob.id); len(got) != 2 {
		t.Fatalf("bob has %d devices", len(got))
	}
}

func TestAnAccountCanHaveTenDevicesNotEleven(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice := e.member("alice") // one device already
	ctx := context.Background()
	for i := 1; i < MaxDevices; i++ {
		if _, err := upsert(ctx, e.pool, alice.id, sub("/d"+strconv.Itoa(i), newBrowser(t))); err != nil {
			t.Fatalf("device %d: %v", i+1, err)
		}
	}
	_, err := upsert(ctx, e.pool, alice.id, sub("/one-too-many", newBrowser(t)))
	if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != http.StatusConflict {
		t.Fatalf("eleventh device: %v", err)
	}
	// Registering a device it already has is not a new device.
	if _, err := upsert(ctx, e.pool, alice.id, sub("/d1", newBrowser(t))); err != nil {
		t.Fatalf("refreshing a known device: %v", err)
	}
}

func TestRemovingADeviceNeedsToOwnIt(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	ctx := context.Background()
	aliceDevices, _ := list(ctx, e.pool, alice.id)
	if gone, err := remove(ctx, e.pool, bob.id, aliceDevices[0].ID); err != nil || gone {
		t.Fatalf("bob removed alice's device: %v %v", gone, err)
	}
	if gone, err := remove(ctx, e.pool, alice.id, uuid.New()); err != nil || gone {
		t.Fatalf("an unknown device: %v %v", gone, err)
	}
	if gone, err := remove(ctx, e.pool, alice.id, aliceDevices[0].ID); err != nil || !gone {
		t.Fatalf("alice removing her own: %v %v", gone, err)
	}
}

func TestThePreviewChoiceIsStoredPerAccount(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	ctx := context.Background()
	if on, err := preview(ctx, e.pool, alice.id); err != nil || !on {
		t.Fatalf("default %v %v", on, err)
	}
	if err := setPreview(ctx, e.pool, alice.id, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := preview(ctx, e.pool, alice.id); on {
		t.Fatal("not stored")
	}
	if on, _ := preview(ctx, e.pool, bob.id); !on {
		t.Fatal("bob's choice changed too")
	}
	if on, err := preview(ctx, e.pool, uuid.New()); err != nil || !on {
		t.Fatalf("unknown account: %v %v", on, err)
	}
}
