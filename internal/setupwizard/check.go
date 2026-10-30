package setupwizard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"

	"github.com/msch128/mnema-talk/internal/config"
)

// checkExisting runs the server's required-settings check on the .env in dir,
// as docker compose would hand it to the app, and prints the result. It
// returns false when there is no .env, and the problems it found.
func checkExisting(out io.Writer, dir string) (found bool, problems []config.Problem, err error) {
	path := filepath.Join(dir, ".env")
	if _, statErr := os.Stat(path); statErr != nil {
		return false, nil, nil
	}
	env, err := godotenv.Read(path)
	if err != nil {
		return true, nil, fmt.Errorf("%s: %w", path, err)
	}
	problems = config.RequiredProblems(config.ComposeEnv(env))
	if len(problems) == 0 {
		fmt.Fprintf(out, "%s: all required settings are present.\n", path)
		return true, nil, nil
	}
	fmt.Fprintln(out, (&config.ConfigError{Problems: problems}).Error())
	return true, problems, nil
}
