package app

import (
	"runtime"
	"strings"
	"testing"
)

func releaseAssetsForTest() []githubAsset {
	names := []string{
		"dongle.FlowWeaver_1.31.0.0_x64_x86_arm64.zip",
		"FlowWeaver-1.31.0-arm64Setup.exe",
		"FlowWeaver-1.31.0-x64Setup.exe",
		"FlowWeaver-1.31.0-x86Setup.exe",
		"flowweaver-cli-darwin-amd64.zip",
		"flowweaver-cli-darwin-arm64.zip",
		"flowweaver-cli-linux-amd64.tar.gz",
		"flowweaver-cli-linux-arm64.tar.gz",
		"flowweaver-cli-windows-386.zip",
		"flowweaver-cli-windows-amd64.zip",
		"flowweaver-cli-windows-arm64.zip",
		"flowweaver-linux-amd64.tar.gz",
		"flowweaver-linux-arm64.tar.gz",
		"flowweaver-linux-x64.tar.gz",
		"flowweaver-windows-386.zip",
		"flowweaver-windows-amd64.zip",
		"flowweaver-windows-arm64.zip",
		"unsigned_dongle.FlowWeaver_1.31.0.0_x64_x86_arm64.msixbundle",
		"dongle.FlowWeaver_1.29.1.0_x64_x86_arm64.7z",
		"flowweaver-cli-windows-amd64.7z",
		"flowweaver-windows-amd64.7z",
	}
	assets := make([]githubAsset, 0, len(names))
	for _, name := range names {
		assets = append(assets, githubAsset{Name: name, Size: 1, DownloadURL: "https://example.invalid/" + name})
	}
	return assets
}

func TestFilterUpdateAssetsKeepsCurrentTargetOnly(t *testing.T) {
	got := filterUpdateAssets(releaseAssetsForTest())
	if len(got) == 0 {
		t.Fatalf("no asset returned for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	wantOS := runtime.GOOS
	wantArch := runtimeAssetArch()
	for _, asset := range got {
		lower := strings.ToLower(asset.Name)
		if strings.HasPrefix(lower, "dongle.") || strings.Contains(lower, "unsigned") {
			t.Errorf("bundle/unsigned asset leaked into update list: %s", asset.Name)
		}
		if isCLIAsset(lower) != headlessBuild {
			t.Errorf("asset from the other flavour leaked into update list: %s", asset.Name)
		}
		if osName := assetOSToken(lower); osName != wantOS {
			t.Errorf("asset for %q leaked into %q update list: %s", osName, wantOS, asset.Name)
		}
		if arch := assetArchToken(lower); arch != wantArch {
			t.Errorf("asset for %q leaked into %q update list: %s", arch, wantArch, asset.Name)
		}
	}
}

func TestFilterUpdateAssetsWindowsAmd64Shortlist(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" || headlessBuild {
		t.Skip("shortlist assertion is for the desktop windows/amd64 build")
	}
	got := filterUpdateAssets(releaseAssetsForTest())
	// Installer first, then the zip every Windows 10 can open, then the
	// legacy 7z kept for old releases.
	want := []string{
		"FlowWeaver-1.31.0-x64Setup.exe",
		"flowweaver-windows-amd64.zip",
		"flowweaver-windows-amd64.7z",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected asset count: got %d (%v), want %d (%v)", len(got), assetNames(got), len(want), want)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("asset %d = %s, want %s", i, got[i].Name, name)
		}
	}
	wantKinds := []string{"exe", "zip", "7z"}
	for i, kind := range wantKinds {
		if got[i].Kind != kind {
			t.Errorf("asset %d kind = %s, want %s", i, got[i].Kind, kind)
		}
	}
}

func TestFilterUpdateAssetsLegacyReleaseStillOffers7z(t *testing.T) {
	if runtime.GOOS != "windows" || headlessBuild {
		t.Skip("legacy archive assertion is for the desktop windows build")
	}
	legacy := []githubAsset{
		{Name: "FlowWeaver-1.29.1-x64Setup.exe", Size: 1, DownloadURL: "https://example.invalid/installer.exe"},
		{Name: "flowweaver-windows-amd64.7z", Size: 1, DownloadURL: "https://example.invalid/portable.7z"},
		{Name: "flowweaver-windows-amd64.zip", Size: 1, DownloadURL: "https://example.invalid/portable.zip"},
	}
	got := filterUpdateAssets(legacy)
	if len(got) < 2 {
		t.Fatalf("expected both archives to be offered, got %v", assetNames(got))
	}
	var kinds []string
	for _, asset := range got {
		kinds = append(kinds, asset.Kind)
	}
	if kinds[len(kinds)-1] != "7z" {
		t.Fatalf("legacy 7z must stay selectable last, got %v", kinds)
	}
}

func TestAssetArchToken(t *testing.T) {
	cases := map[string]string{
		"flowweaver-windows-amd64.zip":           "x64",
		"flowweaver-windows-386.zip":             "x86",
		"flowweaver-windows-arm64.zip":           "arm64",
		"flowweaver-windows-amd64.7z":            "x64",
		"FlowWeaver-1.29.1-x86Setup.exe":         "x86",
		"FlowWeaver-1.29.1-x64Setup.exe":         "x64",
		"flowweaver-linux-x64.tar.gz":            "x64",
		"flowweaver-cli-darwin-arm64.zip":        "arm64",
		"flowweaver-cli-linux-amd64.tar.gz":      "x64",
		"FlowWeaver-1.30.0-alpha.1-x64Setup.exe": "x64",
	}
	for name, want := range cases {
		if got := assetArchToken(strings.ToLower(name)); got != want {
			t.Errorf("assetArchToken(%q) = %q, want %q", name, got, want)
		}
	}
}

func assetNames(assets []ReleaseAsset) []string {
	names := make([]string, len(assets))
	for i, asset := range assets {
		names[i] = asset.Name
	}
	return names
}
