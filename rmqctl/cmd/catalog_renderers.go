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
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	toolcatalog "github.com/apache/rocketmq-dashboard/rmqctl/internal/catalog"
	"github.com/apache/rocketmq-dashboard/rmqctl/internal/output"
)

func renderTable(w io.Writer, tool toolcatalog.Tool, result any, cluster string) error {
	if tool.ViewHint != "table" {
		return output.JSON(w, result)
	}
	rows, err := tableRows(result, tool.TableDataKey)
	if err != nil {
		return fmt.Errorf("tool %s returned invalid table output: %w", tool.Name, err)
	}
	if len(rows) == 0 {
		resource := tool.CLI.Resource
		if cluster != "" {
			fmt.Fprintf(w, "No %s found in cluster %s.\n", resource, cluster)
		} else {
			fmt.Fprintf(w, "No %s found.\n", resource)
		}
		return nil
	}
	return output.Rows(w, rows, tableColumns(rows, tool.TableColumns))
}

func tableRows(result any, dataKey string) ([]map[string]any, error) {
	if dataKey == "" {
		return output.MapsFromAny(result)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected an object containing %q, got %T", dataKey, result)
	}
	value, exists := payload[dataKey]
	if !exists {
		return nil, fmt.Errorf("missing table data field %q", dataKey)
	}
	return output.MapsFromAny(value)
}

func tableColumns(rows []map[string]any, preferredOrder []string) []output.Column {
	keys := make(map[string]struct{})
	for _, row := range rows {
		for key := range row {
			keys[key] = struct{}{}
		}
	}
	var names []string
	seen := make(map[string]bool, len(preferredOrder))
	for _, name := range preferredOrder {
		if _, exists := keys[name]; !exists {
			continue
		}
		names = append(names, name)
		seen[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(keys)) {
		if seen[name] {
			continue
		}
		names = append(names, name)
	}
	columns := make([]output.Column, 0, len(names))
	for _, name := range names {
		columns = append(columns, output.Column{Header: titleHeader(name), Key: name})
	}
	return columns
}

// titleHeader converts a camelCase or snake_case field name into a
// human-friendly Title Case column header, e.g. "clusterId" -> "ClusterId",
// "writeQueues" -> "WriteQueues", "confirm_token" -> "ConfirmToken".
func titleHeader(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i == 0 {
			b.WriteRune(toUpper(r))
			continue
		}
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}
