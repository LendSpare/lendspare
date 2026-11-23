package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The product is LendSpare: its link scheme, its config directory and the Store package agree.

func TestOnlyTheLendSpareSchemeIsALink(t *testing.T) {
	const q = "://connect?origin=share.brainybrain.com&intent=abcdefghijklmnopqrstu"
	if _, _, err := parseDeepLink("lendspare" + q); err != nil {
		t.Fatalf("lendspare:// must be accepted: %v", err)
	}
	if _, _, err := parseDeepLink("share" + q); err == nil {
		t.Fatal("the old share:// scheme must be refused")
	}
}

func TestConfigDirIsNamedLendSpare(t *testing.T) {
	t.Setenv("LENDSPARE_CONFIG_DIR", "")
	if got := filepath.Base(configDir()); got != "LendSpare" {
		t.Fatalf("config dir %q", configDir())
	}
	dir := t.TempDir()
	t.Setenv("LENDSPARE_CONFIG_DIR", dir)
	if configDir() != dir {
		t.Fatalf("LENDSPARE_CONFIG_DIR not honoured: %q", configDir())
	}
}

func TestMsixManifestDeclaresLendSpare(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/msix/AppxManifest.xml.in")
	if err != nil {
		t.Fatal(err)
	}
	m := string(raw)
	for _, want := range []string{
		`<Application Id="LendSpare" Executable="lendspare.exe"`,
		`<uap:Protocol Name="lendspare">`,
		`<desktop:ExecutionAlias Alias="lendspare.exe" />`,
		`TaskId="LendSpareStartup"`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %s", want)
		}
	}
	if strings.Contains(strings.ToLower(m), "share.exe") || strings.Contains(m, `Name="share"`) {
		t.Error("manifest still names the old share program")
	}
}

func TestMsixLogosHaveTheirDeclaredSizes(t *testing.T) {
	sizes := map[string][2]int{
		"StoreLogo.png": {50, 50}, "Square44x44Logo.png": {44, 44},
		"Square150x150Logo.png": {150, 150}, "Wide310x150Logo.png": {310, 150},
		"Square44x44Logo.targetsize-16.png": {16, 16}, "Square44x44Logo.targetsize-32.png": {32, 32},
		"Square44x44Logo.targetsize-256.png":                  {256, 256},
		"Square44x44Logo.targetsize-256_altform-unplated.png": {256, 256},
	}
	for name, want := range sizes {
		f, err := os.Open(filepath.Join("../../packaging/msix/Assets", name))
		if err != nil {
			t.Error(err)
			continue
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width != want[0] || cfg.Height != want[1] {
			t.Errorf("%s: %dx%d, want %dx%d (%v)", name, cfg.Width, cfg.Height, want[0], want[1], err)
		}
	}
}
