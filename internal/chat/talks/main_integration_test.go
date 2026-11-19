//go:build integration

package talks

import (
	"os"
	"testing"

	"github.com/mnema-xyz/talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }
