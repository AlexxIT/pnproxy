package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAction(t *testing.T) {
	name, params := ParseAction("static address 192.168.1.123")
	require.Equal(t, "static", name)
	require.Equal(t, map[string][]string{
		"address": {"192.168.1.123"},
	}, params)
}
