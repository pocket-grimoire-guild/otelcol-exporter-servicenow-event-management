// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapperCodeIsSplitByResponsibility(t *testing.T) {
	requiredFiles := []string{
		"mapper.go",
		"mapper_additional_info.go",
		"mapper_fields.go",
		"mapper_identity.go",
		"mapper_resolution_state.go",
		"mapper_severity.go",
	}
	for _, file := range requiredFiles {
		_, err := os.Stat(filepath.Clean(file))
		require.NoError(t, err, "expected mapper responsibility file %s to exist", file)
	}

	contents, err := os.ReadFile(filepath.Clean("mapper.go"))
	require.NoError(t, err)
	require.LessOrEqual(t, len(strings.Split(string(contents), "\n")), 180, "mapper.go should stay focused on log traversal and record assembly")
}
