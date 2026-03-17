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

package agentendpoint

import (
	"context"
	"os/exec"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	"cloud.google.com/go/osconfig/agentendpoint/apiv1/agentendpointpb"
)

type agentEndpointServiceExecTestServer struct {
	agentendpointpb.UnimplementedAgentEndpointServiceServer
	lastReportTaskCompleteRequest *agentendpointpb.ReportTaskCompleteRequest
}

func (*agentEndpointServiceExecTestServer) ReceiveTaskNotification(req *agentendpointpb.ReceiveTaskNotificationRequest, srv agentendpointpb.AgentEndpointService_ReceiveTaskNotificationServer) error {
	return status.Errorf(codes.Unimplemented, "method ReceiveTaskNotification not implemented")
}

func (*agentEndpointServiceExecTestServer) StartNextTask(ctx context.Context, req *agentendpointpb.StartNextTaskRequest) (*agentendpointpb.StartNextTaskResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartNextTask not implemented")
}

func (*agentEndpointServiceExecTestServer) ReportTaskProgress(ctx context.Context, req *agentendpointpb.ReportTaskProgressRequest) (*agentendpointpb.ReportTaskProgressResponse, error) {
	return &agentendpointpb.ReportTaskProgressResponse{}, nil
}

func (s *agentEndpointServiceExecTestServer) ReportTaskComplete(ctx context.Context, req *agentendpointpb.ReportTaskCompleteRequest) (*agentendpointpb.ReportTaskCompleteResponse, error) {
	s.lastReportTaskCompleteRequest = req
	return &agentendpointpb.ReportTaskCompleteResponse{}, nil
}

func (*agentEndpointServiceExecTestServer) RegisterAgent(ctx context.Context, req *agentendpointpb.RegisterAgentRequest) (*agentendpointpb.RegisterAgentResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RegisterAgent not implemented")
}

func (*agentEndpointServiceExecTestServer) ReportInventory(ctx context.Context, req *agentendpointpb.ReportInventoryRequest) (*agentendpointpb.ReportInventoryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReportInventory not implemented")
}

func outputGen(id string, msg string, st agentendpointpb.ExecStepTaskOutput_State, exitCode int32) *agentendpointpb.ReportTaskCompleteRequest {
	if msg != "" {
		msg = "Error running ExecStepTask: " + msg
	}
	return &agentendpointpb.ReportTaskCompleteRequest{
		TaskId:       id,
		TaskType:     agentendpointpb.TaskType_EXEC_STEP_TASK,
		ErrorMessage: msg,
		Output: &agentendpointpb.ReportTaskCompleteRequest_ExecStepTaskOutput{
			ExecStepTaskOutput: &agentendpointpb.ExecStepTaskOutput{State: st, ExitCode: exitCode},
		},
		InstanceIdToken: testIDToken,
	}
}

func TestRunExecStep(t *testing.T) {
	ctx := context.Background()
	srv := &agentEndpointServiceExecTestServer{}
	tc, err := newTestClient(ctx, srv)
	if err != nil {
		t.Fatal(err)
	}
	defer tc.close()

	tests := []struct {
		name       string
		goos       string
		wantComReq *agentendpointpb.ReportTaskCompleteRequest
		wantPath   string
		wantArgs   []string
		step       *agentendpointpb.ExecStep
	}{
		// Matching script and OS.
		{"LinuxExec", "linux", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "foo", []string{"foo"}, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}}}},
		{"LinuxShell", "linux", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), sh, []string{sh, "foo"}, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_SHELL}}},
		{"LinuxPowerShell", "linux", outputGen("", errLinuxPowerShell.Error(), agentendpointpb.ExecStepTaskOutput_COMPLETED, -1), "", nil, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_POWERSHELL}}},
		{"WinExec", "windows", outputGen("", errWinNoInt.Error(), agentendpointpb.ExecStepTaskOutput_COMPLETED, -1), "", nil, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}}}},
		{"WinShell", "windows", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), winCmd, []string{winCmd, "/c", "foo"}, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_SHELL}}},
		{"WinPowerShell", "windows", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), winPowershell, []string{winPowershell, "-NonInteractive", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "foo"}, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_POWERSHELL}}},
		// Mismatched script and OS.
		{"LinuxExec", "windows", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}}}},
		{"LinuxShell", "windows", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_SHELL}}},
		{"LinuxPowerShell", "windows", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{LinuxExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_POWERSHELL}}},
		{"WinExec", "linux", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}}}},
		{"WinShell", "linux", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_SHELL}}},
		{"WinPowerShell", "linux", outputGen("", "", agentendpointpb.ExecStepTaskOutput_COMPLETED, 0), "", nil, &agentendpointpb.ExecStep{WindowsExecStepConfig: &agentendpointpb.ExecStepConfig{Executable: &agentendpointpb.ExecStepConfig_LocalPath{LocalPath: "foo"}, Interpreter: agentendpointpb.ExecStepConfig_POWERSHELL}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotArgs []string
			run = func(cmd *exec.Cmd) ([]byte, error) {
				gotPath = cmd.Path
				gotArgs = cmd.Args
				return nil, nil
			}
			goos = tt.goos

			if err := tc.client.RunExecStep(ctx, &agentendpointpb.Task{TaskDetails: &agentendpointpb.Task_ExecStepTask{ExecStepTask: &agentendpointpb.ExecStepTask{ExecStep: tt.step}}}); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tt.wantComReq, srv.lastReportTaskCompleteRequest, protocmp.Transform()); diff != "" {
				t.Fatalf("ReportTaskCompleteRequest mismatch (-want +got):\n%s", diff)
			}

			if gotPath != tt.wantPath {
				t.Errorf("did not get expected path, want: %q, got: %q", tt.wantPath, gotPath)
			}

			if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
				t.Fatalf("did not get expected args (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetGCSObject(t *testing.T) {
	ctx := context.Background()
	testContent := "test script content"
	bucket := "test-bucket"
	object := "scripts/test.sh"

	// emulate GCS
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/storage/v1/b/%s/o/%s", bucket, "scripts%2Ftest.sh") {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(testContent))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name": "%s", "bucket": "%s"}`, object, bucket)
	}))
	defer ts.Close()

	// use STORAGE_EMULATOR_HOST
	// https://docs.cloud.google.com/go/docs/reference/cloud.google.com/go/storage/latest#hdr-Creating_a_Client
	os.Setenv("STORAGE_EMULATOR_HOST", ts.URL)
	defer os.Unsetenv("STORAGE_EMULATOR_HOST")

	localPath, err := getGCSObject(ctx, bucket, object, 0)
	if err != nil {
		t.Fatalf("getGCSObject failed: %v", err)
	}
	defer os.Remove(localPath)

	content, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Content mismatch: got %q, want %q", string(content), testContent)
	}

	if filepath.Base(localPath) != "test.sh" {
		t.Errorf("Expected filename test.sh, got %s", filepath.Base(localPath))
	}
}
