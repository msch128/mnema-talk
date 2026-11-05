//go:build integration

package talks

import (
	"os"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }
