package iso_test

// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Golden tests for the FileBuilder placeholder output and determinism.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"shoal/internal/provisioner/iso"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestFileBuilder_PlaceholderDeterminism(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)

	jobID := "job-0001"
	recipe := []byte(`{"task_target":"install-linux.target","target_disk":"/dev/sda"}`)
	assets := iso.Assets{
		RecipeSchema: []byte(`{"$id":"https://example.com/recipe.schema.json","type":"object"}`),
		UserData:     []byte("#cloud-config\nhostname: test\n"),
		UnattendXML:  []byte("<unattend version=\"1.0\"/>"),
		Kickstart:    []byte("lang en_US"),
	}

	// Build #1
	res1, err := builder.BuildTaskISO(context.Background(), jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO (1) failed: %v", err)
	}
	if res1.Path != filepath.Join(root, jobID, "task.iso") {
		t.Fatalf("unexpected path:\n got: %s\nwant: %s", res1.Path, filepath.Join(root, jobID, "task.iso"))
	}
	content1, err := os.ReadFile(res1.Path)
	if err != nil {
		t.Fatalf("read task.iso (1): %v", err)
	}
	hash1 := sha256Hex(content1)
	if hash1 != res1.SHA256 {
		t.Fatalf("result SHA mismatch:\n res: %s\ncalc: %s", res1.SHA256, hash1)
	}

	// Compose expected golden content for the placeholder with all assets present.
	// FileBuilder writes a deterministic, line-oriented manifest with:
	// - header (3 lines)
	// - files section (sorted by file name)
	// - layout section (fixed order: recipe.json, recipe.schema.json, user-data, unattend.xml, ks.cfg)
	exp := goldenPlaceholder(jobID, recipe, assets)
	if !bytes.Equal(exp, content1) {
		t.Fatalf("placeholder content mismatch:\n--- expected ---\n%s\n--- actual ---\n%s", string(exp), string(content1))
	}

	// Build #2 with identical inputs should produce identical bytes and SHA.
	res2, err := builder.BuildTaskISO(context.Background(), jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO (2) failed: %v", err)
	}
	content2, err := os.ReadFile(res2.Path)
	if err != nil {
		t.Fatalf("read task.iso (2): %v", err)
	}
	if !bytes.Equal(content1, content2) {
		t.Fatalf("content changed between identical builds")
	}
	if res1.SHA256 != res2.SHA256 {
		t.Fatalf("SHA changed between identical builds: %s vs %s", res1.SHA256, res2.SHA256)
	}
	if res1.Size != res2.Size {
		t.Fatalf("size changed between identical builds: %d vs %d", res1.Size, res2.Size)
	}

	// Build #3 with different recipe should produce different bytes and SHA.
	jobID2 := "job-0002"
	recipe2 := []byte(`{"task_target":"install-windows.target","target_disk":"\\\\.\\PhysicalDrive0"}`)
	res3, err := builder.BuildTaskISO(context.Background(), jobID2, recipe2, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO (3) failed: %v", err)
	}
	content3, err := os.ReadFile(res3.Path)
	if err != nil {
		t.Fatalf("read task.iso (3): %v", err)
	}
	if bytes.Equal(content1, content3) {
		t.Fatalf("content unexpectedly identical after recipe change")
	}
	if res1.SHA256 == res3.SHA256 {
		t.Fatalf("SHA unexpectedly identical after recipe change")
	}
}

// goldenPlaceholder reconstructs the expected placeholder content written by FileBuilder
// for the given jobID and assets. This must stay in sync with composePlaceholder logic.
func goldenPlaceholder(jobID string, recipe []byte, a iso.Assets) []byte {
	// Header
	var b bytes.Buffer
	b.WriteString("shoal-task-iso-placeholder/1\n")
	b.WriteString("kind: placeholder\n")
	b.WriteString("job_id: ")
	b.WriteString(jobID)
	b.WriteByte('\n')

	// Files (sorted by name)
	type part struct {
		name string
		data []byte
	}
	parts := []part{
		{name: "recipe.json", data: recipe},
	}
	if len(a.RecipeSchema) > 0 {
		parts = append(parts, part{name: "recipe.schema.json", data: a.RecipeSchema})
	}
	if len(a.UserData) > 0 {
		parts = append(parts, part{name: "user-data", data: a.UserData})
	}
	if len(a.UnattendXML) > 0 {
		parts = append(parts, part{name: "unattend.xml", data: a.UnattendXML})
	}
	if len(a.Kickstart) > 0 {
		parts = append(parts, part{name: "ks.cfg", data: a.Kickstart})
	}
	// sort.Slice inline (keep dependency minimal here)
	for i := 0; i < len(parts)-1; i++ {
		for j := i + 1; j < len(parts); j++ {
			if parts[j].name < parts[i].name {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}

	b.WriteString("files:\n")
	for _, p := range parts {
		b.WriteString("  - name: ")
		b.WriteString(p.name)
		b.WriteByte('\n')
		b.WriteString("    sha256: ")
		b.WriteString(sha256Hex(p.data))
		b.WriteByte('\n')
		b.WriteString("    size: ")
		b.WriteString(fmt.Sprintf("%d", len(p.data)))
		b.WriteByte('\n')
	}

	// Layout (fixed order)
	b.WriteString("layout:\n")
	b.WriteString("  - /recipe.json\n")
	if len(a.RecipeSchema) > 0 {
		b.WriteString("  - /recipe.schema.json\n")
	}
	if len(a.UserData) > 0 {
		b.WriteString("  - /user-data\n")
	}
	if len(a.UnattendXML) > 0 {
		b.WriteString("  - /unattend.xml\n")
	}
	if len(a.Kickstart) > 0 {
		b.WriteString("  - /ks.cfg\n")
	}

	return b.Bytes()
}
