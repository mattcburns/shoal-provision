package iso

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

// Package iso provides abstractions for building the per-job task ISO
// artifact described in the provisioner design documents. In Phase 1,
// this includes a stub file-based builder that creates a deterministic
// placeholder "task.iso" file that the controller can serve to BMCs.
// The placeholder is not a bootable ISO image; it exists to unblock
// orchestration, storage lifecycle, and API wiring until the real ISO
// generation pipeline (023) is implemented.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Builder produces a task ISO artifact for a job, embedding the recipe
// and optional additional assets. Real implementations should produce a
// bootable ISO with the specified files. The Phase 1 stub produces a
// deterministic placeholder file at {Root}/{jobID}/task.iso.
type Builder interface {
	// BuildTaskISO generates a task ISO for the given job ID and inputs.
	// Returns the absolute path to the produced file, its size (bytes),
	// and the SHA256 digest (hex).
	BuildTaskISO(ctx context.Context, jobID string, recipe json.RawMessage, assets Assets) (*Result, error)
}

// Assets are optional additional files that may be embedded into the task ISO.
// Field names mirror the canonical file names described in 023.
type Assets struct {
	RecipeSchema []byte // recipe.schema.json (optional)
	UserData     []byte // user-data (cloud-init)
	UnattendXML  []byte // unattend.xml (Windows)
	Kickstart    []byte // ks.cfg (ESXi / Linux kickstart)
	// Future: controller/agent configs, firmware blobs, etc.
}

// Result describes the produced artifact.
type Result struct {
	Path   string // absolute path to task.iso
	Size   int64  // size in bytes
	SHA256 string // hex-encoded SHA256 of file contents
}

// FileBuilder is a Phase 1 stub that creates a deterministic, text-based
// placeholder file named "task.iso" under {Root}/{jobID}/. It also writes
// the input assets as separate files for debugging and golden tests.
type FileBuilder struct {
	Root string // TASK_ISO_DIR, e.g. /var/lib/shoal/task-isos
}

// Ensure FileBuilder satisfies the interface.
var _ Builder = (*FileBuilder)(nil)

// NewFileBuilder constructs a new file-based builder rooted at dir.
// The directory will be created on demand by BuildTaskISO.
func NewFileBuilder(dir string) *FileBuilder {
	return &FileBuilder{Root: dir}
}

var uuidLike = regexp.MustCompile(`^[A-Za-z0-9\-_.:]+$`)

// BuildTaskISO creates a deterministic placeholder file at:
//
//	{Root}/{jobID}/task.iso
//
// It also writes the following sibling files when provided:
//   - recipe.json
//   - recipe.schema.json (if assets.RecipeSchema is set)
//   - user-data            (if assets.UserData is set)
//   - unattend.xml         (if assets.UnattendXML is set)
//   - ks.cfg               (if assets.Kickstart is set)
//
// Determinism notes:
//   - No timestamps are written to the artifact.
//   - Content layout is stable and fields are sorted.
//   - Hashes of each input component are included to bind content.
func (b *FileBuilder) BuildTaskISO(ctx context.Context, jobID string, recipe json.RawMessage, assets Assets) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.Root == "" {
		return nil, errors.New("iso: root directory is empty")
	}
	if jobID == "" || !uuidLike.MatchString(jobID) {
		return nil, fmt.Errorf("iso: invalid jobID %q", jobID)
	}
	if len(recipe) == 0 || string(recipe) == "null" {
		return nil, errors.New("iso: recipe is required")
	}

	// Prepare destination paths
	jobDir := filepath.Join(b.Root, jobID)
	outPath := filepath.Join(jobDir, "task.iso")

	// Ensure directory exists
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return nil, fmt.Errorf("iso: create job directory: %w", err)
	}

	// Write auxiliary files to assist debugging/tests.
	if err := writeIfNonEmpty(filepath.Join(jobDir, "recipe.json"), recipe); err != nil {
		return nil, err
	}
	if len(assets.RecipeSchema) > 0 {
		if err := writeIfNonEmpty(filepath.Join(jobDir, "recipe.schema.json"), assets.RecipeSchema); err != nil {
			return nil, err
		}
	}
	if len(assets.UserData) > 0 {
		if err := writeIfNonEmpty(filepath.Join(jobDir, "user-data"), assets.UserData); err != nil {
			return nil, err
		}
	}
	if len(assets.UnattendXML) > 0 {
		if err := writeIfNonEmpty(filepath.Join(jobDir, "unattend.xml"), assets.UnattendXML); err != nil {
			return nil, err
		}
	}
	if len(assets.Kickstart) > 0 {
		if err := writeIfNonEmpty(filepath.Join(jobDir, "ks.cfg"), assets.Kickstart); err != nil {
			return nil, err
		}
	}

	// Compose deterministic placeholder content.
	content, err := b.composePlaceholder(jobID, recipe, assets)
	if err != nil {
		return nil, err
	}

	// Write atomically
	if err := writeAtomic(outPath, content, 0o644); err != nil {
		return nil, fmt.Errorf("iso: write placeholder: %w", err)
	}

	// Stat and hash
	fi, err := os.Stat(outPath)
	if err != nil {
		return nil, fmt.Errorf("iso: stat output: %w", err)
	}
	sum := sha256.Sum256(content)

	return &Result{
		Path:   outPath,
		Size:   fi.Size(),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

// composePlaceholder builds a deterministic byte slice that documents
// the intended ISO contents and binds the inputs via SHA256 digests.
// The format is a simple line-oriented text for easy inspection.
func (b *FileBuilder) composePlaceholder(jobID string, recipe json.RawMessage, assets Assets) ([]byte, error) {
	var buf bytes.Buffer

	// Header
	buf.WriteString("shoal-task-iso-placeholder/1\n")
	buf.WriteString("kind: placeholder\n")
	buf.WriteString("job_id: ")
	buf.WriteString(jobID)
	buf.WriteByte('\n')

	// Inputs section; keep keys in a stable order.
	type part struct {
		Name string
		Data []byte
	}
	parts := []part{
		{"recipe.json", recipe},
	}
	if len(assets.RecipeSchema) > 0 {
		parts = append(parts, part{"recipe.schema.json", assets.RecipeSchema})
	}
	if len(assets.UserData) > 0 {
		parts = append(parts, part{"user-data", assets.UserData})
	}
	if len(assets.UnattendXML) > 0 {
		parts = append(parts, part{"unattend.xml", assets.UnattendXML})
	}
	if len(assets.Kickstart) > 0 {
		parts = append(parts, part{"ks.cfg", assets.Kickstart})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })

	buf.WriteString("files:\n")
	for _, p := range parts {
		sum := sha256.Sum256(p.Data)
		buf.WriteString("  - name: ")
		buf.WriteString(p.Name)
		buf.WriteByte('\n')
		buf.WriteString("    sha256: ")
		buf.WriteString(hex.EncodeToString(sum[:]))
		buf.WriteByte('\n')
		buf.WriteString("    size: ")
		buf.WriteString(fmt.Sprintf("%d", len(p.Data)))
		buf.WriteByte('\n')
	}

	// Minimal manifest of how a real ISO would be laid out.
	buf.WriteString("layout:\n")
	buf.WriteString("  - /recipe.json\n")
	if len(assets.RecipeSchema) > 0 {
		buf.WriteString("  - /recipe.schema.json\n")
	}
	if len(assets.UserData) > 0 {
		buf.WriteString("  - /user-data\n")
	}
	if len(assets.UnattendXML) > 0 {
		buf.WriteString("  - /unattend.xml\n")
	}
	if len(assets.Kickstart) > 0 {
		buf.WriteString("  - /ks.cfg\n")
	}

	return buf.Bytes(), nil
}

// writeIfNonEmpty writes data to path with 0644 permissions if data is non-empty.
// Uses atomic write semantics to avoid partial writes.
func writeIfNonEmpty(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return writeAtomic(path, data, 0o644)
}

// writeAtomic writes content to a temporary file in the destination
// directory and renames it into place to provide atomicity on POSIX.
func writeAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // best effort
	}()

	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp into place: %w", err)
	}
	return nil
}
