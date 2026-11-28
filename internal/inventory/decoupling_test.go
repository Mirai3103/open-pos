package inventory_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCoreDoesNotImportInventory pins spec §1.1: removing internal/inventory
// must leave the Core POS building.
func TestCoreDoesNotImportInventory(t *testing.T) {
	const module = "github.com/Mirai3103/open-pos/"
	core := []string{
		"internal/sales", "internal/preparation", "internal/catalog", "internal/tables",
		"internal/shift", "internal/auth", "internal/platform/...",
	}
	args := []string{"list", "-deps"}
	for _, p := range core {
		args = append(args, module+p)
	}
	out, err := exec.Command("go", args...).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NotContains(t, string(out), module+"internal/inventory",
		"a Core package imports internal/inventory:\n%s", strings.TrimSpace(string(out)))
}
