package app

import (
	"strings"
	"testing"
)

// The compute confirmation states the limits the terms and the Store listing promise.
func TestComputeConfirmationNamesTheMiningBanAndStepAside(t *testing.T) {
	_, _, details := confirmationText("share.example", "Example", "ai", "", []string{scopeCompute}, "share.example", "", true)
	text := strings.Join(details, "\n")
	for _, want := range []string{"Cryptocurrency mining is never permitted.", "LendSpare steps aside while another program uses the GPU."} {
		if !strings.Contains(text, want) {
			t.Errorf("compute confirmation lacks %q:\n%s", want, text)
		}
	}

	_, _, details = confirmationText("share.example", "Example", "mount", "", []string{scopeFiles}, "share.example", "", true)
	if strings.Contains(strings.Join(details, "\n"), "mining") {
		t.Error("a folders-only connection must not show compute text")
	}
}
