// Package cli holds the identity command-line adapters.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/identity/ports/in"
)

// BootstrapAdmin creates the first admin. The password is read from the
// first line of passwordInput (stdin), so it never appears in the process
// list or the shell history.
func BootstrapAdmin(ctx context.Context, app in.Application, username string, passwordInput io.Reader) error {
	if username == "" {
		return errors.New("--username is required")
	}
	line, err := bufio.NewReader(passwordInput).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return errors.New("no password on standard input")
	}
	return app.Commands.BootstrapAdmin.Handle(ctx, in.BootstrapAdmin{
		UserID:   id.New(),
		Username: username,
		Password: password,
	})
}
