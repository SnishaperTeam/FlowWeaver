package app

import (
	"runtime"
	"strings"
	"testing"
)

func releaseAssetsForTest() []githubAsset {
	names := []string{
		"dongle.FlowWeaver_1.29.1.0_x64_x86_arm64.7z",
		"FlowWeaver-1.29.1-arm64Setup.exe",
		"FlowWeaver-1.29.1-x64Setup.exe",
		"FlowWeaver-1.29.1-x86Setup.exe",
		"flowweaver-cli-darwin-amd64.7z",
		"flowweaver-cli-darwin-arm64.7z",
		"flowweaver-cli-linux-amd64.7z",
		"flowweaver-cli-linux-arm64.7z",
		"flowweaver-cli-windows-386.7z",
		"flowweaver-cli-windows-amd64.7z",
		"flowweaver-cli-windows-arm64.7z",
		"flowweaver-linux-amd64.tar.gz",
		"flowweaver-linux-arm64.tar.gz",
		"flowweaver-linux-x64.tar.gz",
		"flowweaver-windows-386.7z",
		"flowweaver-windows-amd64.7z",
		"flowweaver-windows-arm64.7z",
		"unsigned_dongle.FlowWeaver_1.29.1.0_x64_x86_arm64.msixbundle",
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
	want := []string{"FlowWeaver-1.29.1-x64Setup.exe", "flowweaver-windows-amd64.7z"}
	if len(got) != len(want) {
		t.Fatalf("unexpected asset count: got %d (%v), want %d (%v)", len(got), assetNames(got), len(want), want)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("asset %d = %s, want %s", i, got[i].Name, name)
		}
	}
	if got[0].Kind != "exe" || got[1].Kind != "7z" {
		t.Errorf("unexpected ordering: %s(%s), %s(%s)", got[0].Name, got[0].Kind, got[1].Name, got[1].Kind)
	}
}

func TestAssetArchToken(t *testing.T) {
	cases := map[string]string{
		"flowweaver-windows-amd64.7z":            "x64",
		"flowweaver-windows-386.7z":              "x86",
		"flowweaver-windows-arm64.7z":            "arm64",
		"FlowWeaver-1.29.1-x86Setup.exe":         "x86",
		"FlowWeaver-1.29.1-x64Setup.exe":         "x64",
		"flowweaver-linux-x64.tar.gz":            "x64",
		"flowweaver-cli-darwin-arm64.7z":         "arm64",
		"flowweaver-cli-linux-amd64.7z":          "x64",
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
