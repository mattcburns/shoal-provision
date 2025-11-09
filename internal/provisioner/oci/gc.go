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

package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GCConfig holds garbage collection configuration.
type GCConfig struct {
	// Enabled determines if GC is active
	Enabled bool

	// Interval is how often GC runs (e.g., 1 hour)
	Interval time.Duration

	// GracePeriod is how long unreferenced blobs are kept before deletion
	GracePeriod time.Duration
}

// GarbageCollector manages registry garbage collection.
type GarbageCollector struct {
	storage *Storage
	config  GCConfig
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// GCStats tracks garbage collection statistics.
type GCStats struct {
	StartTime        time.Time
	Duration         time.Duration
	BlobsScanned     int
	BlobsDeleted     int
	BlobsQuarantined int
	BytesFreed       int64
	Errors           []string
}

// quarantineEntry tracks when a blob was quarantined.
type quarantineEntry struct {
	QuarantinedAt time.Time `json:"quarantinedAt"`
	BlobDigest    string    `json:"blobDigest"`
	Size          int64     `json:"size"`
}

// NewGarbageCollector creates a new garbage collector.
func NewGarbageCollector(storage *Storage, config GCConfig) *GarbageCollector {
	return &GarbageCollector{
		storage: storage,
		config:  config,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start begins the garbage collection background process.
func (gc *GarbageCollector) Start() {
	if !gc.config.Enabled {
		close(gc.doneCh)
		return
	}

	go gc.run()
}

// Stop halts the garbage collection process.
func (gc *GarbageCollector) Stop() {
	close(gc.stopCh)
	<-gc.doneCh
}

// run is the main GC loop.
func (gc *GarbageCollector) run() {
	defer close(gc.doneCh)

	ticker := time.NewTicker(gc.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-gc.stopCh:
			return
		case <-ticker.C:
			stats, err := gc.RunGC()
			if err != nil {
				// Log error but continue running
				fmt.Fprintf(os.Stderr, "GC error: %v\n", err)
			} else if stats.BlobsDeleted > 0 || stats.BlobsQuarantined > 0 {
				fmt.Printf("GC completed: deleted=%d, quarantined=%d, freed=%d bytes, duration=%v\n",
					stats.BlobsDeleted, stats.BlobsQuarantined, stats.BytesFreed, stats.Duration)
			}
		}
	}
}

// RunGC performs a single garbage collection cycle.
// This can be called manually or by the background worker.
func (gc *GarbageCollector) RunGC() (*GCStats, error) {
	stats := &GCStats{
		StartTime: time.Now(),
		Errors:    []string{},
	}

	// Step 1: Build reachability graph - find all referenced blobs
	referencedBlobs, err := gc.buildReachabilityGraph()
	if err != nil {
		return stats, fmt.Errorf("failed to build reachability graph: %w", err)
	}

	// Step 2: List all blobs in storage
	allBlobs, err := gc.listAllBlobs()
	if err != nil {
		return stats, fmt.Errorf("failed to list blobs: %w", err)
	}

	stats.BlobsScanned = len(allBlobs)

	// Step 3: Identify unreferenced blobs
	unreferencedBlobs := make(map[string]bool)
	for _, digest := range allBlobs {
		if !referencedBlobs[digest] {
			unreferencedBlobs[digest] = true
		}
	}

	// Step 4: Process quarantine - delete old entries and quarantine new ones
	quarantined, deleted, bytesFreed, errors := gc.processQuarantine(unreferencedBlobs)
	stats.BlobsQuarantined = quarantined
	stats.BlobsDeleted = deleted
	stats.BytesFreed = bytesFreed
	stats.Errors = errors

	stats.Duration = time.Since(stats.StartTime)
	return stats, nil
}

// buildReachabilityGraph builds a set of all blob digests referenced by manifests.
func (gc *GarbageCollector) buildReachabilityGraph() (map[string]bool, error) {
	referenced := make(map[string]bool)

	// List all repositories
	repos, err := gc.listRepositories()
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	// For each repository, find all tags and resolve to manifests
	for _, repo := range repos {
		tags, err := gc.storage.ListTags(repo)
		if err != nil {
			return nil, fmt.Errorf("failed to list tags for %s: %w", repo, err)
		}

		for _, tag := range tags {
			// Get manifest digest from tag
			manifestDigest, err := gc.storage.GetTag(repo, tag)
			if err != nil {
				continue // Skip if tag is broken
			}

			// Mark manifest itself as referenced
			referenced[manifestDigest] = true

			// Get manifest and extract referenced blobs
			manifestData, _, err := gc.storage.GetManifest(repo, manifestDigest)
			if err != nil {
				continue // Skip if manifest can't be read
			}

			// Parse manifest and extract blob references
			var manifest Manifest
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				continue // Skip invalid manifests
			}

			// Mark all referenced blobs
			if manifest.Config != nil && manifest.Config.Digest != "" {
				referenced[manifest.Config.Digest] = true
			}

			for _, layer := range manifest.Layers {
				if layer.Digest != "" {
					referenced[layer.Digest] = true
				}
			}

			for _, blob := range manifest.Blobs {
				if blob.Digest != "" {
					referenced[blob.Digest] = true
				}
			}

			if manifest.Subject != nil && manifest.Subject.Digest != "" {
				referenced[manifest.Subject.Digest] = true
			}
		}
	}

	return referenced, nil
}

// listAllBlobs returns all blob digests in storage.
func (gc *GarbageCollector) listAllBlobs() ([]string, error) {
	blobDir := filepath.Join(gc.storage.root, "blobs", "sha256")

	entries, err := os.ReadDir(blobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read blob directory: %w", err)
	}

	digests := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			// Convert filename to digest format
			digests = append(digests, "sha256:"+entry.Name())
		}
	}

	return digests, nil
}

// listRepositories returns all repository names.
func (gc *GarbageCollector) listRepositories() ([]string, error) {
	repoDir := filepath.Join(gc.storage.root, "repositories")

	entries, err := os.ReadDir(repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read repositories directory: %w", err)
	}

	repos := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			repos = append(repos, entry.Name())
		}
	}

	return repos, nil
}

// processQuarantine moves unreferenced blobs to quarantine and deletes old quarantined blobs.
func (gc *GarbageCollector) processQuarantine(unreferencedBlobs map[string]bool) (int, int, int64, []string) {
	quarantineDir := filepath.Join(gc.storage.root, "quarantine")
	if err := os.MkdirAll(quarantineDir, 0755); err != nil {
		return 0, 0, 0, []string{fmt.Sprintf("failed to create quarantine directory: %v", err)}
	}

	quarantined := 0
	deleted := 0
	var bytesFreed int64
	errors := []string{}

	// Step 1: Check existing quarantine entries and delete those past grace period
	quarantineEntries, err := os.ReadDir(quarantineDir)
	if err == nil {
		for _, entry := range quarantineEntries {
			if entry.IsDir() {
				continue
			}

			entryPath := filepath.Join(quarantineDir, entry.Name())

			// Read quarantine metadata
			data, err := os.ReadFile(entryPath)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to read quarantine entry %s: %v", entry.Name(), err))
				continue
			}

			var qEntry quarantineEntry
			if err := json.Unmarshal(data, &qEntry); err != nil {
				errors = append(errors, fmt.Sprintf("failed to parse quarantine entry %s: %v", entry.Name(), err))
				continue
			}

			// Check if grace period has passed
			if time.Since(qEntry.QuarantinedAt) >= gc.config.GracePeriod {
				// Delete the blob
				if err := gc.storage.DeleteBlob(qEntry.BlobDigest); err != nil {
					errors = append(errors, fmt.Sprintf("failed to delete blob %s: %v", qEntry.BlobDigest, err))
				} else {
					deleted++
					bytesFreed += qEntry.Size

					// Remove quarantine entry
					if err := os.Remove(entryPath); err != nil {
						errors = append(errors, fmt.Sprintf("failed to remove quarantine entry %s: %v", entry.Name(), err))
					}
				}
			}
		}
	}

	// Step 2: Quarantine newly unreferenced blobs
	for digest := range unreferencedBlobs {
		// Check if already in quarantine
		quarantinePath := filepath.Join(quarantineDir, strings.TrimPrefix(digest, "sha256:"))
		if _, err := os.Stat(quarantinePath); err == nil {
			// Already quarantined
			continue
		}

		// Get blob size
		size, err := gc.storage.BlobSize(digest)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to get size for %s: %v", digest, err))
			continue
		}

		// Create quarantine entry
		qEntry := quarantineEntry{
			QuarantinedAt: time.Now(),
			BlobDigest:    digest,
			Size:          size,
		}

		data, err := json.Marshal(qEntry)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to marshal quarantine entry for %s: %v", digest, err))
			continue
		}

		if err := os.WriteFile(quarantinePath, data, 0644); err != nil {
			errors = append(errors, fmt.Sprintf("failed to write quarantine entry for %s: %v", digest, err))
			continue
		}

		quarantined++
	}

	return quarantined, deleted, bytesFreed, errors
}

// ManualGC triggers a manual garbage collection run.
// This is a convenience method that can be exposed via an admin endpoint.
func (gc *GarbageCollector) ManualGC(ctx context.Context) (*GCStats, error) {
	// Run GC in a way that respects context cancellation
	resultCh := make(chan *GCStats, 1)
	errCh := make(chan error, 1)

	go func() {
		stats, err := gc.RunGC()
		if err != nil {
			errCh <- err
		} else {
			resultCh <- stats
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case stats := <-resultCh:
		return stats, nil
	}
}
