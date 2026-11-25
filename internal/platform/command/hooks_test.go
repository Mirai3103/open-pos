package command_test

import (
	"testing"

	"github.com/Mirai3103/open-pos/internal/platform/command"
	"github.com/Mirai3103/open-pos/internal/platform/hooks"
	"github.com/stretchr/testify/assert"
)

func TestRunnerCarriesHooks(t *testing.T) {
	r := command.NewRunner(nil, nil, command.Policy{DenialEventType: "probe.authorization_denied"})
	assert.Nil(t, r.Hooks())

	bus := hooks.New()
	r.SetHooks(bus)
	assert.Same(t, bus, r.Hooks())
}
