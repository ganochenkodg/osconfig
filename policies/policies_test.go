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
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"cloud.google.com/go/osconfig/agentendpoint/apiv1beta/agentendpointpb"
	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/packages"
	utilmocks "github.com/GoogleCloudPlatform/osconfig/util/mocks"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltest"
	"github.com/golang/mock/gomock"
)

// TestChecksum verifies that checksum correctly calculates the SHA256 hash of the input reader.
func TestChecksum(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "basic string",
			data: []byte("test data"),
		},
		{
			name: "empty string",
			data: []byte(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(tt.data)
			h := checksum(r)

			expected := sha256.Sum256(tt.data)
			got := h.Sum(nil)

			if !bytes.Equal(got, expected[:]) {
				t.Errorf("checksum() = %x, want %x", got, expected)
			}
		})
	}
}

// TestWriteIfChanged verifies that writeIfChanged only writes to the file if the content has changed.
func TestWriteIfChanged(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		initialContent []byte
		newContent     []byte
		wantErr        error
	}{
		{
			name:       "new file creation",
			newContent: []byte("content 1"),
			wantErr:    nil,
		},
		{
			name:           "no change",
			initialContent: []byte("content 1"),
			newContent:     []byte("content 1"),
			wantErr:        nil,
		},
		{
			name:           "content update",
			initialContent: []byte("content 1"),
			newContent:     []byte("content 2"),
			wantErr:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			td := t.TempDir()
			path := filepath.Join(td, "test_file")

			if tt.initialContent != nil {
				if err := os.WriteFile(path, tt.initialContent, 0644); err != nil {
					t.Fatalf("failed to setup initial file: %v", err)
				}
			}

			err := writeIfChanged(ctx, tt.newContent, path)
			utiltest.AssertErrorMatch(t, err, tt.wantErr)

			if err == nil {
				utiltest.AssertFileContents(t, path, string(tt.newContent))
			}
		})
	}
}

// TestInstallRecipes verifies that installRecipes correctly iterates over and installs recipes.
func TestInstallRecipes(t *testing.T) {
	uniqueSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())

	tests := []struct {
		name    string
		egp     *agentendpointpb.EffectiveGuestPolicy
		wantErr error
	}{
		{
			name: "no recipes",
			egp:  &agentendpointpb.EffectiveGuestPolicy{},
		},
		{
			name: "recipe with nil software recipe",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				SoftwareRecipes: []*agentendpointpb.EffectiveGuestPolicy_SourcedSoftwareRecipe{
					{
						SoftwareRecipe: nil,
					},
				},
			},
		},
		{
			name: "recipe installation error",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				SoftwareRecipes: []*agentendpointpb.EffectiveGuestPolicy_SourcedSoftwareRecipe{
					{
						SoftwareRecipe: &agentendpointpb.SoftwareRecipe{
							Name: "failing-recipe" + uniqueSuffix,
							InstallSteps: []*agentendpointpb.SoftwareRecipe_Step{
								{
									Step: &agentendpointpb.SoftwareRecipe_Step_FileCopy{
										FileCopy: &agentendpointpb.SoftwareRecipe_Step_CopyFile{
											ArtifactId:  "non-existent",
											Destination: "/tmp/dest",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := installRecipes(ctx, tt.egp)
			utiltest.AssertErrorMatch(t, err, tt.wantErr)
		})
	}
}

// TestSetConfig verifies that setConfig handles different package managers and their configurations.
func TestSetConfig(t *testing.T) {
	dpkgQueryArgs := []string{"-W", "-f", "\\{\"architecture\":\"${Architecture}\",\"package\":\"${Package}\",\"source_name\":\"${source:Package}\",\"source_version\":\"${source:Version}\",\"status\":\"${db:Status-Status}\",\"version\":\"${Version}\"\\}\n"}
	rpmQueryArgs := []string{"--queryformat", "\\{\"architecture\":\"%{ARCH}\",\"package\":\"%{NAME}\",\"source_name\":\"%{SOURCERPM}\",\"version\":\"%|EPOCH?{%{EPOCH}:}:{}|%{VERSION}-%{RELEASE}\"\\}\n", "-a"}
	aptUpgradableArgs := []string{"--just-print", "-qq", "dist-upgrade"}
	yumCheckUpdateArgs := []string{"check-update", "--assumeyes"}
	yumListUpdatesArgs := []string{"update", "--assumeno", "--color=never"}
	zypperListUpdatesArgs := []string{"--gpg-auto-import-keys", "-q", "list-updates"}

	yumCheckUpdateErr := exec.Command("/bin/bash", "-c", "exit 100").Run()

	tests := []struct {
		name         string
		egp          *agentendpointpb.EffectiveGuestPolicy
		aptExists    bool
		yumExists    bool
		zypperExists bool
		googetExists bool
		expectations []expectedCommand
	}{
		{
			name: "empty policy",
			egp:  &agentendpointpb.EffectiveGuestPolicy{},
		},
		{
			name: "apt install",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_APT}},
				},
			},
			aptExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/dpkg-query", dpkgQueryArgs...), stdout: []byte("")},
				{cmd: exec.Command("/usr/bin/apt-get", "update"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
				{cmd: exec.Command("/usr/bin/apt-get", "install", "-y", "p1"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
			},
		},
		{
			name: "apt install manager not found",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_APT}},
				},
			},
			aptExists: false,
		},
		{
			name: "apt update",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_UPDATED, Manager: agentendpointpb.Package_APT}},
				},
			},
			aptExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/dpkg-query", dpkgQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/apt-get", "update"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
				{cmd: exec.Command("/usr/bin/apt-get", aptUpgradableArgs...), envs: []string{"DEBIAN_FRONTEND=noninteractive"}, stdout: []byte("Inst p1 [1.0] (2.0 repo [amd64])\n")},
				{cmd: exec.Command("/usr/bin/apt-get", "install", "-y", "p1"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
			},
		},
		{
			name: "apt remove",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_REMOVED, Manager: agentendpointpb.Package_APT}},
				},
			},
			aptExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/dpkg-query", dpkgQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/apt-get", "remove", "-y", "p1"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
			},
		},
		{
			name: "yum install",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_YUM}},
				},
			},
			yumExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte("")},
				{cmd: exec.Command("/usr/bin/yum", "install", "--assumeyes", "p1")},
			},
		},
		{
			name: "yum update",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_UPDATED, Manager: agentendpointpb.Package_YUM}},
				},
			},
			yumExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/yum", yumCheckUpdateArgs...), err: yumCheckUpdateErr},
				{cmd: exec.Command("/usr/bin/yum", yumListUpdatesArgs...), stdout: []byte("Updating:\n p1 x86_64 2.0 updates 100 k\n")},
				{cmd: exec.Command("/usr/bin/yum", "install", "--assumeyes", "p1")},
			},
		},
		{
			name: "yum remove",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_REMOVED, Manager: agentendpointpb.Package_YUM}},
				},
			},
			yumExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/yum", "remove", "--assumeyes", "p1")},
			},
		},
		{
			name: "zypper install",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_ZYPPER}},
				},
			},
			zypperExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte("")},
				{cmd: exec.Command("/usr/bin/zypper", "--gpg-auto-import-keys", "--non-interactive", "install", "--auto-agree-with-licenses", "p1")},
			},
		},
		{
			name: "zypper update",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_UPDATED, Manager: agentendpointpb.Package_ZYPPER}},
				},
			},
			zypperExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/zypper", zypperListUpdatesArgs...), stdout: []byte("v | Repo | p1 | 1.0 | 2.0 | x86_64\n")},
				{cmd: exec.Command("/usr/bin/zypper", "--gpg-auto-import-keys", "--non-interactive", "install", "--auto-agree-with-licenses", "p1")},
			},
		},
		{
			name: "zypper remove",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_REMOVED, Manager: agentendpointpb.Package_ZYPPER}},
				},
			},
			zypperExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte(`{"package":"p1","status":"installed"}`)},
				{cmd: exec.Command("/usr/bin/zypper", "--non-interactive", "remove", "p1")},
			},
		},
		{
			name: "googet install",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_GOO}},
				},
			},
			googetExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("googet.exe", "installed"), stdout: []byte("")},
				{cmd: exec.Command("googet.exe", "-noconfirm", "install", "p1")},
			},
		},
		{
			name: "googet update",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_UPDATED, Manager: agentendpointpb.Package_GOO}},
				},
			},
			googetExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("googet.exe", "installed"), stdout: []byte("p1.x86_64 1.0\n")},
				{cmd: exec.Command("googet.exe", "update"), stdout: []byte("p1.noarch, 1.0 --> 2.0 from repo\n")},
				{cmd: exec.Command("googet.exe", "-noconfirm", "install", "p1")},
			},
		},
		{
			name: "googet remove",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_REMOVED, Manager: agentendpointpb.Package_GOO}},
				},
			},
			googetExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("googet.exe", "installed"), stdout: []byte("p1.x86_64 1.0\n")},
				{cmd: exec.Command("googet.exe", "-noconfirm", "remove", "p1")},
			},
		},
		{
			name: "package ANY install",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				Packages: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackage{
					{Package: &agentendpointpb.Package{Name: "p1", DesiredState: agentendpointpb.DesiredState_INSTALLED, Manager: agentendpointpb.Package_ANY}},
				},
			},
			aptExists: true, yumExists: true,
			expectations: []expectedCommand{
				{cmd: exec.Command("/usr/bin/dpkg-query", dpkgQueryArgs...), stdout: []byte("")},
				{cmd: exec.Command("/usr/bin/apt-get", "update"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
				{cmd: exec.Command("/usr/bin/apt-get", "install", "-y", "p1"), envs: []string{"DEBIAN_FRONTEND=noninteractive"}},
				{cmd: exec.Command("/usr/bin/rpmquery", rpmQueryArgs...), stdout: []byte("")},
				{cmd: exec.Command("/usr/bin/yum", "install", "--assumeyes", "p1")},
			},
		},
		{
			name: "all repositories",
			egp: &agentendpointpb.EffectiveGuestPolicy{
				PackageRepositories: []*agentendpointpb.EffectiveGuestPolicy_SourcedPackageRepository{
					{PackageRepository: &agentendpointpb.PackageRepository{Repository: &agentendpointpb.PackageRepository_Apt{Apt: &agentendpointpb.AptRepository{Uri: "http://repo"}}}},
					{PackageRepository: &agentendpointpb.PackageRepository{Repository: &agentendpointpb.PackageRepository_Yum{Yum: &agentendpointpb.YumRepository{Id: "repo", BaseUrl: "http://repo"}}}},
					{PackageRepository: &agentendpointpb.PackageRepository{Repository: &agentendpointpb.PackageRepository_Zypper{Zypper: &agentendpointpb.ZypperRepository{Id: "repo", BaseUrl: "http://repo"}}}},
					{PackageRepository: &agentendpointpb.PackageRepository{Repository: &agentendpointpb.PackageRepository_Goo{Goo: &agentendpointpb.GooRepository{Name: "repo", Url: "http://repo"}}}},
				},
			},
			aptExists: true, yumExists: true, zypperExists: true, googetExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockCommandRunner := utilmocks.NewMockCommandRunner(mockCtrl)
			setupSetConfigTest(t, tt.aptExists, tt.yumExists, tt.zypperExists, tt.googetExists, mockCommandRunner)
			setExpectations(mockCommandRunner, tt.expectations)
			setConfig(context.Background(), tt.egp)
		})
	}
}

// TestRun covers the Run function.
func TestRun(t *testing.T) {
	Run(context.Background())
}

// Test_run covers the internal run function.
func Test_run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run(ctx)
}

type expectedCommand struct {
	cmd    *exec.Cmd
	envs   []string
	stdout []byte
	stderr []byte
	err    error
}

type policiesStubOsInfoProvider struct {
	osinfo osinfo.OSInfo
}

func (s policiesStubOsInfoProvider) GetOSInfo(ctx context.Context) (osinfo.OSInfo, error) {
	return s.osinfo, nil
}

func setExpectations(mockCommandRunner *utilmocks.MockCommandRunner, expectedCommandsChain []expectedCommand) {
	if len(expectedCommandsChain) == 0 {
		return
	}

	var prev *gomock.Call
	for _, expectedCmd := range expectedCommandsChain {
		cmd := expectedCmd.cmd
		if len(expectedCmd.envs) > 0 {
			cmd.Env = append(os.Environ(), expectedCmd.envs...)
		}

		if prev == nil {
			prev = mockCommandRunner.EXPECT().
				Run(gomock.Any(), utilmocks.EqCmd(cmd)).
				Return(expectedCmd.stdout, expectedCmd.stderr, expectedCmd.err).Times(1)
		} else {
			prev = mockCommandRunner.EXPECT().
				Run(gomock.Any(), utilmocks.EqCmd(cmd)).
				After(prev).
				Return(expectedCmd.stdout, expectedCmd.stderr, expectedCmd.err).Times(1)
		}
	}
}

func setupSetConfigTest(t *testing.T, apt, yum, zypper, googet bool, runner *utilmocks.MockCommandRunner) {
	oldApt, oldYum, oldZypper, oldGoo := packages.AptExists, packages.YumExists, packages.ZypperExists, packages.GooGetExists
	oldOsInfoProvider := osInfoProvider

	packages.AptExists, packages.YumExists, packages.ZypperExists, packages.GooGetExists = apt, yum, zypper, googet
	packages.SetCommandRunner(runner)
	packages.SetPtyCommandRunner(runner)
	osInfoProvider = policiesStubOsInfoProvider{
		osinfo: osinfo.OSInfo{ShortName: "debian", Version: "11"},
	}

	t.Cleanup(func() {
		packages.AptExists, packages.YumExists, packages.ZypperExists, packages.GooGetExists = oldApt, oldYum, oldZypper, oldGoo
		osInfoProvider = oldOsInfoProvider
	})
}
