/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/rocketmq-dashboard/rmqctl/internal/types"
)

type observedToolCall struct {
	method        string
	path          string
	authorization string
	request       types.ToolCallRequest
}

func TestCatalogMutationRoundTrip(t *testing.T) {
	const confirmToken = "confirmation"
	observed := make(chan observedToolCall, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedToolCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), request}
		mutation := map[string]any{
			"status":        "PLANNED",
			"cluster":       request.Arguments["cluster"],
			"plan":          map[string]any{"summary": "Create topic orders"},
			"confirm_token": confirmToken,
		}
		if dryRun, ok := request.Arguments["dry_run"]; !ok || dryRun != true {
			mutation["status"] = "EXECUTED"
			mutation["result"] = map[string]any{"topic": request.Arguments["topic"]}
		}
		writeStudioSuccess(t, w, mutation)
	}))
	defer server.Close()

	stdout, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"--output", "json",
		"topic", "create", "--dry-run", "--topic", "orders", "--write-queues", "8",
	)
	if exitCode != 0 {
		t.Fatalf("preview failed: %s", stderr)
	}
	call := <-observed
	wantArguments := map[string]any{
		"cluster": "instance-dev", "topic": "orders", "writeQueues": float64(8), "dry_run": true,
	}
	if call.method != http.MethodPost || call.path != "/api/mcp/tools/call" ||
		!strings.HasPrefix(call.authorization, "RMQ-HMAC-SHA256 Credential=test-ak, Signature=") ||
		!reflect.DeepEqual(call.request.Arguments, wantArguments) {
		t.Fatalf("unexpected preview request: %#v", call)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("decode preview output: %v", err)
	}
	if preview["confirm_token"] != confirmToken || preview["plan"] == nil {
		t.Fatalf("unexpected preview output: %#v", preview)
	}

	stdout, stderr, exitCode = executeTestApp(t, server.Client(), server.URL,
		"--output", "json",
		"topic", "create", "--topic", "orders", "--write-queues", "8",
		"--confirm-token", confirmToken,
	)
	if exitCode != 0 {
		t.Fatalf("apply failed: %s", stderr)
	}
	if call := <-observed; call.request.Arguments["dry_run"] != nil ||
		call.request.Arguments["confirm_token"] != confirmToken {
		t.Fatalf("unexpected apply request: %#v", call.request)
	}
	if strings.Contains(stderr, "WARNING") || strings.Contains(stderr, "Preview:") {
		t.Fatalf("confirm-token should skip auto-preview and prompt: %s", stderr)
	}
	var apply map[string]any
	if err := json.Unmarshal([]byte(stdout), &apply); err != nil || apply["status"] != "EXECUTED" {
		t.Fatalf("unexpected apply output: %#v, err=%v", apply, err)
	}
}

func TestCatalogDefaultsClusterToContextInstance(t *testing.T) {
	observed := make(chan types.ToolCallRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if r.Header.Get("X-RMQ-Cluster") != "instance-profile" {
			t.Error("wrong authenticated Instance")
		}
		observed <- request
		writeStudioSuccess(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	_, stderr, exitCode := executeTestAppWithInstance(t, server.Client(), server.URL, "profile",
		"--output", "json", "topic", "list")
	if exitCode != 0 {
		t.Fatalf("context default failed: exit=%d stderr=%s", exitCode, stderr)
	}
	if request := <-observed; request.Arguments["cluster"] != "instance-profile" {
		t.Fatalf("cluster = %v, want context Instance", request.Arguments["cluster"])
	}
}

func TestCatalogPreservesExplicitCluster(t *testing.T) {
	observed := make(chan types.ToolCallRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- request
		writeStudioSuccess(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	_, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"topic", "list", "--cluster", "instance-dev",
	)
	if exitCode != 0 {
		t.Fatalf("explicit cluster failed: exit=%d stderr=%s", exitCode, stderr)
	}
	if cluster := (<-observed).Arguments["cluster"]; cluster != "instance-dev" {
		t.Fatalf("cluster = %#v, want explicit value", cluster)
	}
}

// TestCatalogAllowsL1WithoutYes verifies that L1 (read-only) operations are
// never gated by the confirmation prompt.
func TestCatalogAllowsL1WithoutYes(t *testing.T) {
	observed := make(chan types.ToolCallRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- request
		writeStudioSuccess(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	_, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"topic", "list", "--cluster", "instance-dev",
	)
	if exitCode != 0 {
		t.Fatalf("L1 without --yes should succeed: exit=%d stderr=%s", exitCode, stderr)
	}
	<-observed
}

// TestCatalogInteractiveConfirmAcceptsYes verifies the auto-preview flow:
// running an L2 tool without --dry-run or --confirm-token triggers an
// automatic dry-run, shows the plan on stderr, prompts for confirmation,
// then applies with the returned confirm_token when the user types "yes".
func TestCatalogInteractiveConfirmAcceptsYes(t *testing.T) {
	const confirmToken = "confirmation"
	observed := make(chan observedToolCall, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedToolCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), request}
		mutation := map[string]any{
			"status":        "PLANNED",
			"cluster":       request.Arguments["cluster"],
			"plan":          map[string]any{"summary": "Create topic orders"},
			"confirm_token": confirmToken,
		}
		if dryRun, ok := request.Arguments["dry_run"]; !ok || dryRun != true {
			mutation["status"] = "EXECUTED"
			mutation["result"] = map[string]any{"topic": request.Arguments["topic"]}
		}
		writeStudioSuccess(t, w, mutation)
	}))
	defer server.Close()

	stdout, stderr, exitCode := executeTestAppWithStdin(t, server.Client(), server.URL, "dev",
		"yes\n",
		"--output", "json",
		"topic", "create", "--cluster", "instance-dev", "--topic", "orders", "--write-queues", "8",
	)
	if exitCode != 0 {
		t.Fatalf("auto-preview with yes should proceed: exit=%d stderr=%s", exitCode, stderr)
	}

	// First call: automatic dry-run.
	previewCall := <-observed
	if previewCall.request.Arguments["dry_run"] != true {
		t.Fatalf("first call should be dry-run: %#v", previewCall.request)
	}
	if previewCall.request.Arguments["confirm_token"] != nil {
		t.Fatalf("dry-run should not carry confirm_token: %#v", previewCall.request)
	}

	// Second call: apply with token.
	applyCall := <-observed
	if applyCall.request.Arguments["dry_run"] != nil {
		t.Fatalf("apply should not carry dry_run: %#v", applyCall.request)
	}
	if applyCall.request.Arguments["confirm_token"] != confirmToken {
		t.Fatalf("apply should carry confirm_token: %#v", applyCall.request)
	}
	if applyCall.request.Arguments["topic"] != "orders" {
		t.Fatalf("unexpected apply request: %#v", applyCall.request)
	}

	// stderr should contain the preview plan and the confirmation prompt.
	if !strings.Contains(stderr, "Preview:") {
		t.Fatalf("expected Preview: in stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "Apply this plan?") || !strings.Contains(stderr, "Type \"yes\" to continue:") {
		t.Fatalf("expected apply confirmation prompt in stderr: %s", stderr)
	}
	if strings.Contains(stderr, "WARNING") || strings.Contains(stderr, server.URL) {
		t.Fatalf("auto-preview prompt should not show WARNING or server URL: %s", stderr)
	}

	// stdout should contain only the final EXECUTED JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["status"] != "EXECUTED" {
		t.Fatalf("expected only JSON result in stdout: %s, err=%v", stdout, err)
	}
}

// TestCatalogAutoPreviewWithYesSkipsConfirm verifies that --yes skips the
// interactive prompt but still runs the automatic dry-run + apply cycle.
func TestCatalogAutoPreviewWithYesSkipsConfirm(t *testing.T) {
	const confirmToken = "confirmation"
	observed := make(chan observedToolCall, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedToolCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), request}
		mutation := map[string]any{
			"status":        "PLANNED",
			"cluster":       request.Arguments["cluster"],
			"plan":          map[string]any{"summary": "Create topic orders"},
			"confirm_token": confirmToken,
		}
		if dryRun, ok := request.Arguments["dry_run"]; !ok || dryRun != true {
			mutation["status"] = "EXECUTED"
			mutation["result"] = map[string]any{"topic": request.Arguments["topic"]}
		}
		writeStudioSuccess(t, w, mutation)
	}))
	defer server.Close()

	stdout, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"--yes",
		"--output", "json",
		"topic", "create", "--cluster", "instance-dev", "--topic", "orders", "--write-queues", "8",
	)
	if exitCode != 0 {
		t.Fatalf("--yes auto-preview should succeed: exit=%d stderr=%s", exitCode, stderr)
	}

	// First call: automatic dry-run.
	previewCall := <-observed
	if previewCall.request.Arguments["dry_run"] != true {
		t.Fatalf("first call should be dry-run: %#v", previewCall.request)
	}

	// Second call: apply with token.
	applyCall := <-observed
	if applyCall.request.Arguments["dry_run"] != nil {
		t.Fatalf("apply should not carry dry_run: %#v", applyCall.request)
	}
	if applyCall.request.Arguments["confirm_token"] != confirmToken {
		t.Fatalf("apply should carry confirm_token: %#v", applyCall.request)
	}

	// stderr should contain the preview plan but NOT the confirmation prompt.
	if !strings.Contains(stderr, "Preview:") {
		t.Fatalf("expected Preview: in stderr: %s", stderr)
	}
	if strings.Contains(stderr, "WARNING") || strings.Contains(stderr, "Type \"yes\"") {
		t.Fatalf("--yes should skip confirmation prompt: %s", stderr)
	}

	// stdout should contain only the final EXECUTED JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["status"] != "EXECUTED" {
		t.Fatalf("expected only JSON result in stdout: %s, err=%v", stdout, err)
	}
}

// TestCatalogClusterListOmitsClusterArgument verifies that rmq.cluster.list
// (which has no "cluster" field in its inputSchema) does not get a cluster
// argument injected into the tool call.
func TestCatalogClusterListOmitsClusterArgument(t *testing.T) {
	observed := make(chan types.ToolCallRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		observed <- request
		writeStudioSuccess(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	_, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"--output", "json", "cluster", "list",
	)
	if exitCode != 0 {
		t.Fatalf("cluster list failed: exit=%d stderr=%s", exitCode, stderr)
	}
	request := <-observed
	if _, hasCluster := request.Arguments["cluster"]; hasCluster {
		t.Fatalf("rmq.cluster.list should not carry cluster argument: %#v", request.Arguments)
	}
}

// TestCatalogConfirmTokenSkipsAutoPreview verifies that supplying
// --confirm-token bypasses the auto-preview orchestration entirely and sends
// a single apply request directly to the server.
func TestCatalogConfirmTokenSkipsAutoPreview(t *testing.T) {
	const confirmToken = "confirmation"
	observed := make(chan observedToolCall, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request types.ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedToolCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), request}
		writeStudioSuccess(t, w, map[string]any{
			"status":        "EXECUTED",
			"cluster":       request.Arguments["cluster"],
			"result":        map[string]any{"topic": request.Arguments["topic"]},
			"confirm_token": confirmToken,
		})
	}))
	defer server.Close()

	stdout, stderr, exitCode := executeTestApp(t, server.Client(), server.URL,
		"--output", "json",
		"topic", "create", "--cluster", "instance-dev", "--topic", "orders", "--write-queues", "8",
		"--confirm-token", confirmToken,
	)
	if exitCode != 0 {
		t.Fatalf("confirm-token apply should succeed: exit=%d stderr=%s", exitCode, stderr)
	}

	// Exactly one call: apply with token, no dry-run.
	call := <-observed
	if call.request.Arguments["dry_run"] != nil {
		t.Fatalf("should not carry dry_run: %#v", call.request)
	}
	if call.request.Arguments["confirm_token"] != confirmToken {
		t.Fatalf("should carry confirm_token: %#v", call.request)
	}

	// No auto-preview or prompt.
	if strings.Contains(stderr, "Preview:") || strings.Contains(stderr, "WARNING") {
		t.Fatalf("confirm-token should not trigger auto-preview: %s", stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["status"] != "EXECUTED" {
		t.Fatalf("expected EXECUTED in stdout: %s, err=%v", stdout, err)
	}
}
