package cli

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/product"
)

func TestBrandHelp(t *testing.T) {
	helpHome(t)
	out, err := runOut(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, product.Name+" —") || !strings.Contains(out, "  "+product.ID+" provider add") {
		t.Fatalf("help names the wrong app or command: %s", out)
	}
	if !strings.Contains(out, "magpie://import") {
		t.Fatal("help changed the import protocol")
	}
}
