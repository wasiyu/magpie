//go:build nogui

package cli

import "errors"

func runJellytoolApp() error { return errors.New("Jellytool requires a desktop build") }
