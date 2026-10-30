package portconflict

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestARealSecondBindIsRecognised(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, err = net.Listen("tcp4", l.Addr().String())
	if !InUse(err) {
		t.Fatalf("InUse(%v) = false", err)
	}
	port := fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
	got := Wrap(err, port, "TCP", "PORT").Error()
	for _, want := range []string{"TCP port " + port, "another program", "PORT in .env"} {
		if !strings.Contains(got, want) {
			t.Errorf("message %q lacks %q", got, want)
		}
	}
	if !errors.Is(Wrap(err, port, "TCP", "PORT"), err) {
		t.Error("the original error is lost")
	}
}

func TestOtherErrorsPassThrough(t *testing.T) {
	err := errors.New("permission denied")
	if Wrap(err, "80", "TCP", "PORT") != err || InUse(nil) {
		t.Fatal("an unrelated error must come back unchanged")
	}
}

func TestInUseText(t *testing.T) {
	for in, want := range map[string]bool{
		"listen udp4 0.0.0.0:3479: bind: address already in use":                                    true,
		"listen tcp 0.0.0.0:80: bind: Only one usage of each socket address is normally permitted.": true,
		"connection refused": false,
	} {
		if got := InUseText(in); got != want {
			t.Errorf("InUseText(%q) = %v", in, got)
		}
	}
}
