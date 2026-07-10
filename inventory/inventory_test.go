package inventory

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/osconfig/agentconfig"
	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/packages"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltest"
)

func TestProvider(t *testing.T) {
	osInfo := osinfo.OSInfo{
		Hostname:      "testhost",
		LongName:      "testLong",
		ShortName:     "testShort",
		Version:       "testVersion",
		KernelVersion: "#1 SMP PREEMPT_DYNAMIC Debian 6.1.123-1 (2025-01-02)",
		KernelRelease: "6.1.0-29-cloud-amd64",
		Architecture:  "x86_64",
	}

	updates := packages.Packages{
		Yum: []*packages.PkgInfo{{Name: "YumPkgUpdate", Arch: "Arch", Version: "Version", Type: "rpm", Purl: "pkg:rpm/Namespace/YumPkgUpdate@Version?arch=Arch"}},
		Apt: []*packages.PkgInfo{{Name: "AptPkgUpdate", Arch: "Arch", Version: "Version", Type: "deb", Purl: "pkg:deb/Namespace/AptPkgUpdate@Version?arch=Arch"}},
	}

	installed := packages.Packages{
		Yum:    []*packages.PkgInfo{{Name: "YumInstalledPkg", Arch: "Arch", Version: "Version", Type: "rpm", Purl: "pkg:rpm/Namespace/YumInstalledPkg@Version?arch=Arch"}},
		GooGet: []*packages.PkgInfo{{Name: "GooGetInstalledPkg", Arch: "Arch", Version: "Version", Type: "googet", Purl: "pkg:googet/Namespace/GooGetInstalledPkg@Version?arch=Arch"}},
	}

	tests := []struct {
		name string
		stub *stubProvider
		want *InstanceInventory
	}{
		{
			name: "all providers failed, returns empty result",
			stub: &stubProvider{
				osinfo: func(_ context.Context) (osinfo.OSInfo, error) { return osinfo.OSInfo{}, fmt.Errorf("unexpected error") },
				packageUpdates: func(_ context.Context) (packages.Packages, error) {
					return packages.Packages{}, fmt.Errorf("unexpected error")
				},
				installedPackages: func(_ context.Context) (packages.Packages, error) {
					return packages.Packages{}, fmt.Errorf("unexpected error")
				},
			},
			want: &InstanceInventory{
				InstalledPackages: &packages.Packages{},
				PackageUpdates:    &packages.Packages{},
				LastUpdated:       "1970-01-01T10:00:00Z",
			},
		},
		{
			name: "all providers succeeded, returns all data",
			stub: &stubProvider{
				osinfo: func(_ context.Context) (osinfo.OSInfo, error) { return osInfo, nil },
				packageUpdates: func(_ context.Context) (packages.Packages, error) {
					return updates, nil
				},
				installedPackages: func(_ context.Context) (packages.Packages, error) {
					return installed, nil
				},
			},

			want: &InstanceInventory{
				Hostname:             "testhost",
				LongName:             "testLong",
				ShortName:            "testShort",
				Version:              "testVersion",
				Architecture:         "x86_64",
				KernelVersion:        "#1 SMP PREEMPT_DYNAMIC Debian 6.1.123-1 (2025-01-02)",
				KernelRelease:        "6.1.0-29-cloud-amd64",
				OSConfigAgentVersion: "",
				InstalledPackages: &packages.Packages{
					Yum:    []*packages.PkgInfo{{Name: "YumInstalledPkg", Arch: "Arch", Version: "Version", Type: "rpm", Purl: "pkg:rpm/Namespace/YumInstalledPkg@Version?arch=Arch"}},
					GooGet: []*packages.PkgInfo{{Name: "GooGetInstalledPkg", Arch: "Arch", Version: "Version", Type: "googet", Purl: "pkg:googet/Namespace/GooGetInstalledPkg@Version?arch=Arch"}},
				},
				PackageUpdates: &packages.Packages{
					Yum: []*packages.PkgInfo{{Name: "YumPkgUpdate", Arch: "Arch", Version: "Version", Type: "rpm", Purl: "pkg:rpm/Namespace/YumPkgUpdate@Version?arch=Arch"}},
					Apt: []*packages.PkgInfo{{Name: "AptPkgUpdate", Arch: "Arch", Version: "Version", Type: "deb", Purl: "pkg:deb/Namespace/AptPkgUpdate@Version?arch=Arch"}},
				},
				LastUpdated: "1970-01-01T10:00:00Z",
			},
		},
		{
			name: "some providers succeeded, returns available data",
			stub: &stubProvider{
				osinfo: func(_ context.Context) (osinfo.OSInfo, error) { return osInfo, nil },
				packageUpdates: func(_ context.Context) (packages.Packages, error) {
					return packages.Packages{}, fmt.Errorf("unexpected error")
				},
				installedPackages: func(_ context.Context) (packages.Packages, error) {
					return installed, nil
				},
			},

			want: &InstanceInventory{
				Hostname:             "testhost",
				LongName:             "testLong",
				ShortName:            "testShort",
				Version:              "testVersion",
				Architecture:         "x86_64",
				KernelVersion:        "#1 SMP PREEMPT_DYNAMIC Debian 6.1.123-1 (2025-01-02)",
				KernelRelease:        "6.1.0-29-cloud-amd64",
				OSConfigAgentVersion: "",
				PackageUpdates:       &packages.Packages{},
				InstalledPackages: &packages.Packages{
					Yum:    []*packages.PkgInfo{{Name: "YumInstalledPkg", Arch: "Arch", Version: "Version", Type: "rpm", Purl: "pkg:rpm/Namespace/YumInstalledPkg@Version?arch=Arch"}},
					GooGet: []*packages.PkgInfo{{Name: "GooGetInstalledPkg", Arch: "Arch", Version: "Version", Type: "googet", Purl: "pkg:googet/Namespace/GooGetInstalledPkg@Version?arch=Arch"}},
				},
				LastUpdated: "1970-01-01T10:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := defaultInventoryProvider{
				osInfoProvider:            tt.stub,
				packageUpdatesProvider:    tt.stub,
				installedPackagesProvider: tt.stub,
				clock:                     stubClock{},
			}

			ctx := context.Background()
			got := provider.Get(ctx)

			utiltest.AssertEquals(t, got, tt.want)
		})
	}

}

// TestNewProvider tests the NewProvider constructor.
func TestNewProvider(t *testing.T) {
	t.Cleanup(func() {
		setTraceGetInventory(t, false)
	})

	tests := []struct {
		name              string
		traceGetInventory bool
	}{
		{
			name:              "trace get inventory disabled, want non-nil provider",
			traceGetInventory: false,
		},
		{
			name:              "trace get inventory enabled, want non-nil provider",
			traceGetInventory: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTraceGetInventory(t, tt.traceGetInventory)

			gotProvider := NewProvider()
			defaultProvider, _ := gotProvider.(*defaultInventoryProvider)
			utiltest.AssertEquals(t, defaultProvider.clock, defaultClock{})
		})
	}
}

// TestDefaultClock tests the defaultClock implementation.
func TestDefaultClock(t *testing.T) {
	c := newDefaultClock()
	now := c.Now()
	if now.IsZero() {
		t.Error("expected non-zero time from defaultClock.Now()")
	}
}

// setTraceGetInventory sets the trace-get-inventory configuration via a mock metadata server.
func setTraceGetInventory(t *testing.T, val bool) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"instance":{"attributes":{"trace-get-inventory":"%t"}}}`, val)
	}))
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(ts.URL, "http://"))
	t.Cleanup(ts.Close)

	if err := agentconfig.WatchConfig(context.Background()); err != nil {
		t.Fatalf("WatchConfig() err: %v", err)
	}
}

type stubClock struct{}

func (sc stubClock) Now() time.Time {
	return time.UnixMicro(0).Add(10 * time.Hour)
}

type stubProvider struct {
	osinfo            func(context.Context) (osinfo.OSInfo, error)
	packageUpdates    func(context.Context) (packages.Packages, error)
	installedPackages func(context.Context) (packages.Packages, error)
}

func (p stubProvider) GetOSInfo(ctx context.Context) (osinfo.OSInfo, error) {
	return p.osinfo(ctx)
}

func (p stubProvider) GetInstalledPackages(ctx context.Context) (packages.Packages, error) {
	return p.installedPackages(ctx)
}

func (p stubProvider) GetPackageUpdates(ctx context.Context) (packages.Packages, error) {
	return p.packageUpdates(ctx)
}
