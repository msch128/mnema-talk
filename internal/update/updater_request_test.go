package update

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestUpdaterReportsRequestsItCannotBuildWithoutLeakingTheToken(t *testing.T) {
	u := &Updater{endpoint: "http://updater.invalid/\x7f", token: testToken, client: http.DefaultClient}
	err := u.Trigger(context.Background())
	if err == nil || !strings.Contains(err.Error(), "build updater request") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("unbuildable request: %v", err)
	}
}
