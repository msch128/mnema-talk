// Package portconflict turns "address already in use" into a sentence that
// names the port, says another program holds it and names the setting that
// moves Mnema Talk to a free one.
package portconflict

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
)

// InUse reports whether err says the port is taken by another program.
func InUse(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EADDRINUSE) || InUseText(err.Error())
}

// InUseText is InUse for an error that only survives as text (the TURN relay
// keeps the listen errors of its transports as strings).
func InUseText(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "address already in use") || strings.Contains(s, "only one usage of each socket address")
}

// Hint says what to do about a port that is in use: which port, over which
// protocol, that another program uses it and which variable changes it.
// port may be a range such as "50000-50050".
func Hint(port, proto, env string) string {
	return fmt.Sprintf("%s port %s is already in use by another program on this host (a second copy of Mnema Talk, another web server or a leftover container); stop that program or move Mnema Talk to a free port with %s in .env", proto, port, env)
}

// Wrap returns err with the hint in front when err is a port conflict, and
// err unchanged otherwise.
func Wrap(err error, port, proto, env string) error {
	if !InUse(err) {
		return err
	}
	return fmt.Errorf("%s: %w", Hint(port, proto, env), err)
}
