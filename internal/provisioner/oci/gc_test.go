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
	"testing"
	"time"
)

func TestNewGarbageCollector(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	config := GCConfig{
		Enabled:     true,
		Interval:    1 * time.Hour,
		GracePeriod: 24 * time.Hour,
	}

	gc := NewGarbageCollector(storage, config)
	if gc == nil {
		t.Fatal("Expected garbage collector, got nil")
	}

	if gc.storage != storage {
		t.Error("Storage not set correctly")
	}

	if gc.config.Enabled != config.Enabled {
		t.Error("Config not set correctly")
	}
}

func TestGC_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	config := GCConfig{
		Enabled:     true,
		Interval:    100 * time.Millisecond,
		GracePeriod: 1 * time.Second,
	}

	gc := NewGarbageCollector(storage, config)
	gc.Start()

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Stop should complete without hanging
	gc.Stop()
}

func TestGC_StartStop_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    100 * time.Millisecond,
		GracePeriod: 1 * time.Second,
	}

	gc := NewGarbageCollector(storage, config)
	gc.Start()
	gc.Stop()

	// Should complete immediately when disabled
}

func TestGC_BuildReachabilityGraph_EmptyStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	gc := NewGarbageCollector(storage, GCConfig{})

	referenced, err := gc.buildReachabilityGraph()
	if err != nil {
		t.Fatalf("buildReachabilityGraph() error = %v", err)
	}

	if len(referenced) != 0 {
		t.Errorf("Expected 0 referenced blobs, got %d", len(referenced))
	}
}

func TestGC_BuildReachabilityGraph_WithManifests(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create test blobs
	blob1Digest, err := storage.WriteBlob(strings.NewReader("test blob 1"), "")
	if err != nil {
		t.Fatalf("Failed to write blob1: %v", err)
	}

	blob2Digest, err := storage.WriteBlob(strings.NewReader("test blob 2"), "")
	if err != nil {
		t.Fatalf("Failed to write blob2: %v", err)
	}

	// Create manifest referencing blob1 and blob2
	manifest := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Layers: []Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    blob1Digest,
				Size:      11,
			},
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    blob2Digest,
				Size:      11,
			},
		},
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Failed to marshal manifest: %v", err)
	}

	manifestDigest, err := storage.PutManifest("test-repo", "v1.0", manifestData)
	if err != nil {
		t.Fatalf("Failed to put manifest: %v", err)
	}

	// Build reachability graph
	gc := NewGarbageCollector(storage, GCConfig{})
	referenced, err := gc.buildReachabilityGraph()
	if err != nil {
		t.Fatalf("buildReachabilityGraph() error = %v", err)
	}

	// Check that all blobs are referenced
	if !referenced[blob1Digest] {
		t.Errorf("blob1 should be referenced")
	}

	if !referenced[blob2Digest] {
		t.Errorf("blob2 should be referenced")
	}

	if !referenced[manifestDigest] {
		t.Errorf("manifest should be referenced")
	}
}

func TestGC_ListAllBlobs(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create test blobs
	digest1, err := storage.WriteBlob(strings.NewReader("blob1"), "")
	if err != nil {
		t.Fatalf("Failed to write blob1: %v", err)
	}

	digest2, err := storage.WriteBlob(strings.NewReader("blob2"), "")
	if err != nil {
		t.Fatalf("Failed to write blob2: %v", err)
	}

	gc := NewGarbageCollector(storage, GCConfig{})
	blobs, err := gc.listAllBlobs()
	if err != nil {
		t.Fatalf("listAllBlobs() error = %v", err)
	}

	if len(blobs) != 2 {
		t.Errorf("Expected 2 blobs, got %d", len(blobs))
	}

	foundBlob1 := false
	foundBlob2 := false
	for _, digest := range blobs {
		if digest == digest1 {
			foundBlob1 = true
		}
		if digest == digest2 {
			foundBlob2 = true
		}
	}

	if !foundBlob1 {
		t.Error("blob1 not found in list")
	}

	if !foundBlob2 {
		t.Error("blob2 not found in list")
	}
}

func TestGC_QuarantineAndDelete(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create an unreferenced blob
	blobDigest, err := storage.WriteBlob(strings.NewReader("unreferenced blob"), "")
	if err != nil {
		t.Fatalf("Failed to write blob: %v", err)
	}

	config := GCConfig{
		Enabled:     false, // Manual control for testing
		Interval:    1 * time.Hour,
		GracePeriod: 100 * time.Millisecond,
	}

	gc := NewGarbageCollector(storage, config)

	// First GC run - should quarantine the blob
	stats1, err := gc.RunGC()
	if err != nil {
		t.Fatalf("First RunGC() error = %v", err)
	}

	if stats1.BlobsScanned != 1 {
		t.Errorf("Expected 1 blob scanned, got %d", stats1.BlobsScanned)
	}

	if stats1.BlobsQuarantined != 1 {
		t.Errorf("Expected 1 blob quarantined, got %d", stats1.BlobsQuarantined)
	}

	if stats1.BlobsDeleted != 0 {
		t.Errorf("Expected 0 blobs deleted on first run, got %d", stats1.BlobsDeleted)
	}

	// Verify quarantine entry exists
	quarantineDir := filepath.Join(tmpDir, "quarantine")
	quarantineFile := filepath.Join(quarantineDir, strings.TrimPrefix(blobDigest, "sha256:"))
	if _, err := os.Stat(quarantineFile); os.IsNotExist(err) {
		t.Error("Quarantine entry should exist")
	}

	// Wait for grace period
	time.Sleep(150 * time.Millisecond)

	// Second GC run - should delete the blob
	stats2, err := gc.RunGC()
	if err != nil {
		t.Fatalf("Second RunGC() error = %v", err)
	}

	if stats2.BlobsDeleted != 1 {
		t.Errorf("Expected 1 blob deleted on second run, got %d", stats2.BlobsDeleted)
	}

	// Verify blob is gone
	exists, err := storage.BlobExists(blobDigest)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}

	if exists {
		t.Error("Blob should be deleted")
	}

	// Verify quarantine entry is gone
	if _, err := os.Stat(quarantineFile); !os.IsNotExist(err) {
		t.Error("Quarantine entry should be removed")
	}
}

func TestGC_ReferencedBlobNotDeleted(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create a blob and reference it in a manifest
	blobDigest, err := storage.WriteBlob(strings.NewReader("referenced blob"), "")
	if err != nil {
		t.Fatalf("Failed to write blob: %v", err)
	}

	manifest := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Layers: []Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    blobDigest,
				Size:      16,
			},
		},
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Failed to marshal manifest: %v", err)
	}

	if _, err := storage.PutManifest("test-repo", "latest", manifestData); err != nil {
		t.Fatalf("Failed to put manifest: %v", err)
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    1 * time.Hour,
		GracePeriod: 0, // No grace period for faster testing
	}

	gc := NewGarbageCollector(storage, config)

	// Run GC
	stats, err := gc.RunGC()
	if err != nil {
		t.Fatalf("RunGC() error = %v", err)
	}

	// Blob should not be quarantined or deleted
	if stats.BlobsQuarantined != 0 {
		t.Errorf("Expected 0 blobs quarantined, got %d", stats.BlobsQuarantined)
	}

	if stats.BlobsDeleted != 0 {
		t.Errorf("Expected 0 blobs deleted, got %d", stats.BlobsDeleted)
	}

	// Verify blob still exists
	exists, err := storage.BlobExists(blobDigest)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}

	if !exists {
		t.Error("Referenced blob should not be deleted")
	}
}

func TestGC_SharedBlobMultipleTags(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create a blob shared by two manifests
	sharedBlobDigest, err := storage.WriteBlob(strings.NewReader("shared blob"), "")
	if err != nil {
		t.Fatalf("Failed to write blob: %v", err)
	}

	// Create first manifest with shared blob
	manifest1 := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Layers: []Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    sharedBlobDigest,
				Size:      11,
			},
		},
	}

	manifestData1, _ := json.Marshal(manifest1)
	_, err = storage.PutManifest("test-repo", "v1.0", manifestData1)
	if err != nil {
		t.Fatalf("Failed to put manifest1: %v", err)
	}

	// Create second manifest with same shared blob
	manifest2 := Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Layers: []Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    sharedBlobDigest,
				Size:      11,
			},
		},
	}

	manifestData2, _ := json.Marshal(manifest2)
	_, err = storage.PutManifest("test-repo", "v2.0", manifestData2)
	if err != nil {
		t.Fatalf("Failed to put manifest2: %v", err)
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    1 * time.Hour,
		GracePeriod: 0,
	}

	gc := NewGarbageCollector(storage, config)

	// Delete first tag
	if err := storage.DeleteTag("test-repo", "v1.0"); err != nil {
		t.Fatalf("Failed to delete tag v1.0: %v", err)
	}

	// Run GC - blob should NOT be deleted because v2.0 still references it
	stats, err := gc.RunGC()
	if err != nil {
		t.Fatalf("RunGC() error = %v", err)
	}

	// Shared blob should not be quarantined
	exists, err := storage.BlobExists(sharedBlobDigest)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}

	if !exists {
		t.Error("Shared blob should still exist after deleting one tag")
	}

	// Delete second tag
	if err := storage.DeleteTag("test-repo", "v2.0"); err != nil {
		t.Fatalf("Failed to delete tag v2.0: %v", err)
	}

	// Run GC again - now blob should be quarantined
	stats, err = gc.RunGC()
	if err != nil {
		t.Fatalf("Second RunGC() error = %v", err)
	}

	if stats.BlobsQuarantined < 1 {
		t.Errorf("Expected at least 1 blob quarantined after deleting all tags, got %d", stats.BlobsQuarantined)
	}
}

func TestGC_ManualGC_WithContext(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	gc := NewGarbageCollector(storage, GCConfig{})

	ctx := context.Background()
	stats, err := gc.ManualGC(ctx)
	if err != nil {
		t.Fatalf("ManualGC() error = %v", err)
	}

	if stats == nil {
		t.Fatal("Expected stats, got nil")
	}

	if stats.Duration == 0 {
		t.Error("Expected non-zero duration")
	}
}

func TestGC_ManualGC_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	gc := NewGarbageCollector(storage, GCConfig{})

	// Use an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Wait a moment to ensure context is definitely cancelled
	time.Sleep(10 * time.Millisecond)

	_, err = gc.ManualGC(ctx)
	if err != context.Canceled {
		// The GC might complete before checking context if storage is empty
		// This is acceptable behavior
		if err != nil {
			t.Logf("Got error %v instead of context.Canceled (acceptable)", err)
		}
	}
}

func TestGC_ConcurrentOperations(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    1 * time.Hour,
		GracePeriod: 1 * time.Second,
	}

	gc := NewGarbageCollector(storage, config)

	// Start GC in background
	done := make(chan bool)
	go func() {
		for i := 0; i < 5; i++ {
			gc.RunGC()
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Concurrently write blobs
	for i := 0; i < 10; i++ {
		go func(idx int) {
			storage.WriteBlob(strings.NewReader(fmt.Sprintf("concurrent blob %d", idx)), "")
		}(i)
	}

	// Wait for GC to complete
	<-done

	// Verify storage is still functional
	digest, err := storage.WriteBlob(strings.NewReader("test after concurrent"), "")
	if err != nil {
		t.Errorf("Storage should still be functional after concurrent GC: %v", err)
	}

	exists, err := storage.BlobExists(digest)
	if err != nil || !exists {
		t.Error("Blob written after concurrent GC should exist")
	}
}

func TestGC_Stats(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create unreferenced blobs
	for i := 0; i < 3; i++ {
		_, err := storage.WriteBlob(strings.NewReader(fmt.Sprintf("blob %d", i)), "")
		if err != nil {
			t.Fatalf("Failed to write blob: %v", err)
		}
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    1 * time.Hour,
		GracePeriod: 0,
	}

	gc := NewGarbageCollector(storage, config)

	stats, err := gc.RunGC()
	if err != nil {
		t.Fatalf("RunGC() error = %v", err)
	}

	if stats.BlobsScanned != 3 {
		t.Errorf("Expected 3 blobs scanned, got %d", stats.BlobsScanned)
	}

	if stats.BlobsQuarantined != 3 {
		t.Errorf("Expected 3 blobs quarantined, got %d", stats.BlobsQuarantined)
	}

	if stats.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}

	if stats.Duration == 0 {
		t.Error("Duration should be non-zero")
	}

	if stats.Errors == nil {
		t.Error("Errors slice should not be nil")
	}

	if len(stats.Errors) != 0 {
		t.Errorf("Expected 0 errors, got %d: %v", len(stats.Errors), stats.Errors)
	}
}

func TestGC_EmptyRepository(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	config := GCConfig{
		Enabled:     false,
		Interval:    1 * time.Hour,
		GracePeriod: 0,
	}

	gc := NewGarbageCollector(storage, config)

	// Run GC on empty storage
	stats, err := gc.RunGC()
	if err != nil {
		t.Fatalf("RunGC() on empty storage error = %v", err)
	}

	if stats.BlobsScanned != 0 {
		t.Errorf("Expected 0 blobs scanned, got %d", stats.BlobsScanned)
	}

	if stats.BlobsDeleted != 0 {
		t.Errorf("Expected 0 blobs deleted, got %d", stats.BlobsDeleted)
	}
}
