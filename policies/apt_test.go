//  Copyright 2019 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package policies

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/osconfig/agentendpoint/apiv1beta/agentendpointpb"
	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltest"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/packet"
)

func TestAptRepositories(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-gpg-key"))
	}))
	defer srv.Close()

	tests := []struct {
		name       string
		repos      []*agentendpointpb.AptRepository
		osProvider osinfo.Provider
		want       string
		wantErr    error
	}{
		{
			name: "no repositories",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "10"
			}},
			repos:   []*agentendpointpb.AptRepository{},
			want:    "# Repo file managed by Google OSConfig agent\n",
			wantErr: nil,
		},
		{
			name: "single deb repo, debian 10",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "10"
			}},
			repos: []*agentendpointpb.AptRepository{
				{Uri: "http://repo1-url/", Distribution: "distribution", Components: []string{"component1"}},
			},
			want:    "# Repo file managed by Google OSConfig agent\n\ndeb http://repo1-url/ distribution component1\n",
			wantErr: nil,
		},
		{
			name: "single deb repo, debian 12",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "12"
			}},
			repos: []*agentendpointpb.AptRepository{
				{Uri: "http://repo1-url/", Distribution: "distribution", Components: []string{"component1"}},
			},
			want:    "# Repo file managed by Google OSConfig agent\n\ndeb [signed-by=/etc/apt/trusted.gpg.d/osconfig_agent_managed.gpg] http://repo1-url/ distribution component1\n",
			wantErr: nil,
		},
		{
			name: "unknown archive type defaults to deb",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "10"
			}},
			repos: []*agentendpointpb.AptRepository{
				{Uri: "http://repo", Distribution: "dist", ArchiveType: agentendpointpb.AptRepository_ArchiveType(99)},
			},
			want:    "# Repo file managed by Google OSConfig agent\n\ndeb http://repo dist\n",
			wantErr: nil,
		},
		{
			name: "multiple repos and components",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "10"
			}},
			repos: []*agentendpointpb.AptRepository{
				{Uri: "http://repo1", Distribution: "dist1", Components: []string{"comp1"}, ArchiveType: agentendpointpb.AptRepository_DEB_SRC},
				{Uri: "http://repo2", Distribution: "dist2", Components: []string{"comp1", "comp2"}},
			},
			want:    "# Repo file managed by Google OSConfig agent\n\ndeb-src http://repo1 dist1 comp1\n\ndeb http://repo2 dist2 comp1 comp2\n",
			wantErr: nil,
		},
		{
			name: "repo with gpg key (coverage check)",
			osProvider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "10"
			}},
			repos: []*agentendpointpb.AptRepository{
				{Uri: "http://repo", Distribution: "dist", GpgKey: srv.URL},
			},
			want:    "# Repo file managed by Google OSConfig agent\n\ndeb http://repo dist\n",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			osInfoProviderActual := osInfoProvider
			defer func() { osInfoProvider = osInfoProviderActual }()
			osInfoProvider = tt.osProvider

			td, err := ioutil.TempDir(os.TempDir(), "")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(td)
			testRepo := filepath.Join(td, "testRepo")

			err = aptRepositories(ctx, tt.repos, testRepo)
			utiltest.AssertErrorMatch(t, err, tt.wantErr)
			utiltest.AssertFileContents(t, testRepo, tt.want)
		})
	}
}

func TestGetAptGPGKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			w.Header().Set("Content-Length", "2000000")
			w.Write(make([]byte, 100))
		case "/binary":
			w.Write([]byte{0x99, 0x01, 0x02})
		case "/empty_armored":
			w.Write([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\n-----END PGP PUBLIC KEY BLOCK-----"))
		default:
			w.Write([]byte("invalid data"))
		}
	}))
	defer srv.Close()

	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{
			name:    "empty armored key",
			url:     srv.URL + "/empty_armored",
			wantErr: nil,
		},
		{
			name:    "invalid data attempt",
			url:     srv.URL + "/invalid",
			wantErr: errors.New("openpgp: invalid data: tag byte does not have MSB set"),
		},
		{
			name:    "binary key attempt",
			url:     srv.URL + "/binary",
			wantErr: errors.New("unexpected EOF"),
		},
		{
			name:    "key too large",
			url:     srv.URL + "/large",
			wantErr: errors.New("key size of 2000000 too large"),
		},
		{
			name:    "http get error",
			url:     "http://invalid:url",
			wantErr: errors.New(`parse "http://invalid:url": invalid port ":url" after host`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getAptGPGKey(tt.url)
			if err != nil {
				// openpgp returns custom error types
				// convert to standard error type for AssertErrorMatch
				err = errors.New(err.Error())
			}
			utiltest.AssertErrorMatch(t, err, tt.wantErr)
		})
	}
}

func TestUseSignedBy(t *testing.T) {
	tests := []struct {
		name     string
		repo     *agentendpointpb.AptRepository
		expected string
	}{
		{
			"1 repo",
			&agentendpointpb.AptRepository{Uri: "http://repo1-url/", Distribution: "distribution", Components: []string{"component1"}},
			"\ndeb [signed-by=/etc/apt/trusted.gpg.d/osconfig_agent_managed.gpg] http://repo1-url/ distribution component1",
		},
		{
			"2 components",
			&agentendpointpb.AptRepository{Uri: "http://repo2-url/", Distribution: "distribution", Components: []string{"component1", "component2"}, ArchiveType: agentendpointpb.AptRepository_DEB},
			"\ndeb [signed-by=/etc/apt/trusted.gpg.d/osconfig_agent_managed.gpg] http://repo2-url/ distribution component1 component2",
		},
	}

	useSignedBy := true
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utiltest.AssertEquals(t, getAptRepoLine(tt.repo, useSignedBy), tt.expected)
		})
	}
}

func TestIsArmoredGPGKey(t *testing.T) {
	tests := []struct {
		name     string
		keyData  []byte
		expected bool
	}{
		{
			name:     "valid armored key",
			keyData:  []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQENBF2..."),
			expected: true,
		},
		{
			name:     "invalid armored key (not a key)",
			keyData:  []byte("-----BEGIN PGP MESSAGE-----\n\n..."),
			expected: true, // armor.Decode returns true for any valid armored block
		},
		{
			name:     "binary data",
			keyData:  []byte{0x99, 0x01, 0x02, 0x03},
			expected: false,
		},
		{
			name:     "empty data",
			keyData:  []byte{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utiltest.AssertEquals(t, isArmoredGPGKey(tt.keyData), tt.expected)
		})
	}
}

func TestContainsEntity(t *testing.T) {
	e1 := &openpgp.Entity{PrimaryKey: &packet.PublicKey{Fingerprint: [20]byte{1}}}
	e2 := &openpgp.Entity{PrimaryKey: &packet.PublicKey{Fingerprint: [20]byte{2}}}
	e3 := &openpgp.Entity{PrimaryKey: &packet.PublicKey{Fingerprint: [20]byte{3}}}

	tests := []struct {
		name     string
		es       []*openpgp.Entity
		e        *openpgp.Entity
		expected bool
	}{
		{
			name:     "entity is present",
			es:       []*openpgp.Entity{e1, e2},
			e:        e1,
			expected: true,
		},
		{
			name:     "entity is not present",
			es:       []*openpgp.Entity{e1, e2},
			e:        e3,
			expected: false,
		},
		{
			name:     "empty entity list",
			es:       []*openpgp.Entity{},
			e:        e1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utiltest.AssertEquals(t, containsEntity(tt.es, tt.e), tt.expected)
		})
	}
}

func TestReadInstanceOsInfo(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		provider    osinfo.Provider
		wantName    string
		wantVersion float64
		wantErr     error
	}{
		{
			name: "successful read debian 11",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "11"
			}},
			wantName:    "debian",
			wantVersion: 11,
			wantErr:     nil,
		},
		{
			name:     "provider error",
			provider: errorOsInfoProvider{},
			wantErr:  errors.New("error getting osinfo: osinfo error"),
		},
		{
			name: "invalid version string",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "not-a-number"
			}},
			wantName:    "debian",
			wantVersion: 0,
			wantErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			osInfoProviderActual := osInfoProvider
			defer func() { osInfoProvider = osInfoProviderActual }()
			osInfoProvider = tt.provider

			gotName, gotVersion, gotErr := readInstanceOsInfo(ctx)
			utiltest.AssertErrorMatch(t, gotErr, tt.wantErr)
			utiltest.AssertEquals(t, gotName, tt.wantName)
			utiltest.AssertEquals(t, gotVersion, tt.wantVersion)
		})
	}
}

func TestShouldUseSignedBy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		provider osinfo.Provider
		expected bool
	}{
		{
			name: "debian 12",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "12"
			}},
			expected: true,
		},
		{
			name: "debian 11",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "debian", "Debian", "11"
			}},
			expected: false,
		},
		{
			name: "ubuntu 24",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "ubuntu", "Ubuntu", "24"
			}},
			expected: true,
		},
		{
			name: "ubuntu 22",
			provider: stubOsInfoProvider{nameVersionProvider: func() (string, string, string) {
				return "ubuntu", "Ubuntu", "22"
			}},
			expected: false,
		},
		{
			name:     "error reading os info",
			provider: errorOsInfoProvider{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			osInfoProviderActual := osInfoProvider
			defer func() { osInfoProvider = osInfoProviderActual }()
			osInfoProvider = tt.provider

			utiltest.AssertEquals(t, shouldUseSignedBy(ctx), tt.expected)
		})
	}
}

type errorOsInfoProvider struct{}

func (e errorOsInfoProvider) GetOSInfo(ctx context.Context) (osinfo.OSInfo, error) {
	return osinfo.OSInfo{}, errors.New("osinfo error")
}

type stubOsInfoProvider struct {
	nameVersionProvider func() (string, string, string)
}

func (s stubOsInfoProvider) GetOSInfo(ctx context.Context) (osinfo.OSInfo, error) {
	short, long, version := s.nameVersionProvider()

	return osinfo.OSInfo{
		Hostname:      "test",
		LongName:      long,
		ShortName:     short,
		Version:       version,
		KernelVersion: "test",
		KernelRelease: "test",
		Architecture:  "x86_64",
	}, nil
}
