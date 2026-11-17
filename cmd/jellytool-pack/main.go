// jellytool-pack copies a locally supplied, pinned Bun into app resources.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/yetone/magpie/internal/plugin"
)

func main() {
	if len(os.Args) != 3 && len(os.Args) != 5 || len(os.Args) == 5 && os.Args[3] != "--target" {
		fmt.Fprintln(os.Stderr, "usage: jellytool-pack /absolute/bun /absolute/empty-output [--target goos/goarch]")
		os.Exit(2)
	}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	if len(os.Args) == 5 {
		parts := strings.Split(os.Args[4], "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintln(os.Stderr, "target must be goos/goarch")
			os.Exit(2)
		}
		goos, goarch = parts[0], parts[1]
	}
	var err error
	if len(os.Args) == 3 {
		err = plugin.PackJellytoolPlugins(context.Background(), os.Args[1], os.Args[2])
	} else {
		err = plugin.PackJellytoolPluginsFor(context.Background(), os.Args[1], os.Args[2], goos, goarch)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
