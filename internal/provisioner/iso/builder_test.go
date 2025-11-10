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

// TestFileBuilder_ErrorPaths validates error handling for invalid inputs.
func TestFileBuilder_ErrorPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)
	ctx := context.Background()
	validRecipe := []byte(`{"task_target":"install-linux.target"}`)
	validAssets := iso.Assets{}

	// Empty jobID
	_, err := builder.BuildTaskISO(ctx, "", validRecipe, validAssets)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("invalid jobID")) {
		t.Fatalf("expected invalid jobID error, got %v", err)
	}

	// Invalid jobID format (not UUID-like)
	_, err = builder.BuildTaskISO(ctx, "invalid-id!", validRecipe, validAssets)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("invalid jobID")) {
		t.Fatalf("expected invalid jobID error, got %v", err)
	}

	// Empty recipe
	_, err = builder.BuildTaskISO(ctx, "job-0001", []byte{}, validAssets)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("recipe is required")) {
		t.Fatalf("expected recipe required error, got %v", err)
	}

	// Null recipe
	_, err = builder.BuildTaskISO(ctx, "job-0002", []byte("null"), validAssets)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("recipe is required")) {
		t.Fatalf("expected recipe required error for null, got %v", err)
	}

	// Canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = builder.BuildTaskISO(canceledCtx, "job-0003", validRecipe, validAssets)
	if err == nil || err != context.Canceled {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}

	// Empty root directory
	emptyBuilder := iso.NewFileBuilder("")
	_, err = emptyBuilder.BuildTaskISO(ctx, "job-0004", validRecipe, validAssets)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("root directory is empty")) {
		t.Fatalf("expected empty root error, got %v", err)
	}
}

// TestFileBuilder_MinimalAssets validates builds with minimal assets.
func TestFileBuilder_MinimalAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)
	ctx := context.Background()

	jobID := "job-minimal"
	recipe := []byte(`{"task_target":"install-linux.target"}`)
	// No optional assets
	assets := iso.Assets{}

	res, err := builder.BuildTaskISO(ctx, jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO with minimal assets failed: %v", err)
	}

	// Verify output exists and is valid
	content, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read task.iso: %v", err)
	}

	// Should contain only recipe in placeholder
	if !bytes.Contains(content, []byte("recipe.json")) {
		t.Fatal("expected recipe.json in placeholder")
	}
	// Should NOT contain optional assets
	if bytes.Contains(content, []byte("user-data")) {
		t.Fatal("unexpected user-data in minimal placeholder")
	}
	if bytes.Contains(content, []byte("unattend.xml")) {
		t.Fatal("unexpected unattend.xml in minimal placeholder")
	}
	if bytes.Contains(content, []byte("ks.cfg")) {
		t.Fatal("unexpected ks.cfg in minimal placeholder")
	}
}

// TestFileBuilder_AllAssets validates builds with all optional assets populated.
func TestFileBuilder_AllAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)
	ctx := context.Background()

	jobID := "job-full"
	recipe := []byte(`{"task_target":"install-windows.target"}`)
	assets := iso.Assets{
		RecipeSchema: []byte(`{"$id":"https://example.com/recipe.schema.json","type":"object"}`),
		UserData:     []byte("#cloud-config\nhostname: full-test\n"),
		UnattendXML:  []byte("<unattend version=\"1.0\"><settings/></unattend>"),
		Kickstart:    []byte("lang en_US\nkeyboard us\n"),
	}

	res, err := builder.BuildTaskISO(ctx, jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO with all assets failed: %v", err)
	}

	// Verify output contains all assets
	content, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read task.iso: %v", err)
	}

	required := []string{"recipe.json", "recipe.schema.json", "user-data", "unattend.xml", "ks.cfg"}
	for _, asset := range required {
		if !bytes.Contains(content, []byte(asset)) {
			t.Fatalf("expected %s in full placeholder", asset)
		}
	}

	// Verify auxiliary files were written to job directory
	jobDir := filepath.Join(root, jobID)
	for _, file := range []string{"recipe.json", "recipe.schema.json", "user-data", "unattend.xml", "ks.cfg"} {
		path := filepath.Join(jobDir, file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected auxiliary file %s, got error: %v", file, err)
		}
	}
}

// TestFileBuilder_DeterminismWithSourceDateEpoch validates SOURCE_DATE_EPOCH handling.
// Note: The current FileBuilder implementation is already deterministic (no timestamps).
// This test documents that behavior and ensures it remains stable.
func TestFileBuilder_DeterminismWithSourceDateEpoch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)
	ctx := context.Background()

	jobID := "job-epoch"
	recipe := []byte(`{"task_target":"install-linux.target","target_disk":"/dev/sda"}`)
	assets := iso.Assets{
		UserData: []byte("#cloud-config\nhostname: epoch-test\n"),
	}

	// Set SOURCE_DATE_EPOCH environment variable for reproducibility
	oldEnv := os.Getenv("SOURCE_DATE_EPOCH")
	os.Setenv("SOURCE_DATE_EPOCH", "1609459200") // 2021-01-01 00:00:00 UTC
	defer func() {
		if oldEnv != "" {
			os.Setenv("SOURCE_DATE_EPOCH", oldEnv)
		} else {
			os.Unsetenv("SOURCE_DATE_EPOCH")
		}
	}()

	// Build #1
	res1, err := builder.BuildTaskISO(ctx, jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO (1) failed: %v", err)
	}
	content1, err := os.ReadFile(res1.Path)
	if err != nil {
		t.Fatalf("read task.iso (1): %v", err)
	}
	hash1 := res1.SHA256

	// Remove the ISO to force rebuild
	if err := os.Remove(res1.Path); err != nil {
		t.Fatalf("remove task.iso: %v", err)
	}

	// Build #2 with same SOURCE_DATE_EPOCH
	res2, err := builder.BuildTaskISO(ctx, jobID, recipe, assets)
	if err != nil {
		t.Fatalf("BuildTaskISO (2) failed: %v", err)
	}
	content2, err := os.ReadFile(res2.Path)
	if err != nil {
		t.Fatalf("read task.iso (2): %v", err)
	}
	hash2 := res2.SHA256

	// Verify deterministic output
	if !bytes.Equal(content1, content2) {
		t.Fatal("content changed between builds with same SOURCE_DATE_EPOCH")
	}
	if hash1 != hash2 {
		t.Fatalf("SHA changed between builds with same SOURCE_DATE_EPOCH: %s vs %s", hash1, hash2)
	}
}

// TestFileBuilder_ConcurrentBuilds validates thread safety for concurrent builds.
func TestFileBuilder_ConcurrentBuilds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builder := iso.NewFileBuilder(root)
	ctx := context.Background()

	recipe := []byte(`{"task_target":"install-linux.target"}`)
	assets := iso.Assets{
		UserData: []byte("#cloud-config\nhostname: concurrent\n"),
	}

	// Build multiple ISOs concurrently
	const numJobs = 10
	type result struct {
		jobID string
		res   *iso.Result
		err   error
	}
	results := make(chan result, numJobs)

	for i := 0; i < numJobs; i++ {
		jobID := fmt.Sprintf("job-concurrent-%02d", i)
		go func(id string) {
			res, err := builder.BuildTaskISO(ctx, id, recipe, assets)
			results <- result{jobID: id, res: res, err: err}
		}(jobID)
	}

	// Collect results
	for i := 0; i < numJobs; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent build %s failed: %v", r.jobID, r.err)
		}
		if r.res == nil {
			t.Errorf("concurrent build %s returned nil result", r.jobID)
		}
	}
}
