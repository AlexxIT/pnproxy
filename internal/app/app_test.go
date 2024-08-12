package app

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAction(t *testing.T) {
	name, params := ParseAction("static address 192.168.1.123")
	require.Equal(t, "static", name)
	require.Equal(t, url.Values{
		"address": {"192.168.1.123"},
	}, params)
}
