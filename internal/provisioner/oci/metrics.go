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
	"sync"
	"time"
)

// Metrics holds OCI registry metrics for Prometheus export.
type Metrics struct {
	mu sync.RWMutex

	// Upload/download metrics
	uploadBytesTotal   int64
	downloadBytesTotal int64
	uploadCount        int64
	downloadCount      int64

	// Request metrics
	blobGetCount     int64
	blobHeadCount    int64
	blobPutCount     int64
	manifestGetCount int64
	manifestPutCount int64
	manifestDelCount int64

	// Timing metrics (in milliseconds)
	uploadDurationTotal   int64
	downloadDurationTotal int64

	// Storage metrics
	storageBytes int64
	blobCount    int64

	// GC metrics
	gcRunsTotal    int64
	gcBlobsDeleted int64
	gcLastRunTime  time.Time
	gcLastDuration time.Duration

	// Error metrics
	uploadErrorCount   int64
	downloadErrorCount int64
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		gcLastRunTime: time.Time{},
	}
}

// RecordUpload records a blob upload.
func (m *Metrics) RecordUpload(bytes int64, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.uploadBytesTotal += bytes
	m.uploadCount++
	m.uploadDurationTotal += duration.Milliseconds()
}

// RecordDownload records a blob download.
func (m *Metrics) RecordDownload(bytes int64, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloadBytesTotal += bytes
	m.downloadCount++
	m.downloadDurationTotal += duration.Milliseconds()
}

// RecordBlobGet records a blob GET request.
func (m *Metrics) RecordBlobGet() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobGetCount++
}

// RecordBlobHead records a blob HEAD request.
func (m *Metrics) RecordBlobHead() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobHeadCount++
}

// RecordBlobPut records a blob PUT request.
func (m *Metrics) RecordBlobPut() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobPutCount++
}

// RecordManifestGet records a manifest GET request.
func (m *Metrics) RecordManifestGet() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifestGetCount++
}

// RecordManifestPut records a manifest PUT request.
func (m *Metrics) RecordManifestPut() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifestPutCount++
}

// RecordManifestDelete records a manifest DELETE request.
func (m *Metrics) RecordManifestDelete() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifestDelCount++
}

// RecordUploadError records an upload error.
func (m *Metrics) RecordUploadError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploadErrorCount++
}

// RecordDownloadError records a download error.
func (m *Metrics) RecordDownloadError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloadErrorCount++
}

// UpdateStorageMetrics updates storage-related metrics.
func (m *Metrics) UpdateStorageMetrics(bytes int64, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.storageBytes = bytes
	m.blobCount = count
}

// RecordGCRun records a garbage collection run.
func (m *Metrics) RecordGCRun(blobsDeleted int64, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gcRunsTotal++
	m.gcBlobsDeleted += blobsDeleted
	m.gcLastRunTime = time.Now()
	m.gcLastDuration = duration
}

// GetMetrics returns a snapshot of current metrics.
func (m *Metrics) GetMetrics() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		UploadBytesTotal:      m.uploadBytesTotal,
		DownloadBytesTotal:    m.downloadBytesTotal,
		UploadCount:           m.uploadCount,
		DownloadCount:         m.downloadCount,
		BlobGetCount:          m.blobGetCount,
		BlobHeadCount:         m.blobHeadCount,
		BlobPutCount:          m.blobPutCount,
		ManifestGetCount:      m.manifestGetCount,
		ManifestPutCount:      m.manifestPutCount,
		ManifestDeleteCount:   m.manifestDelCount,
		UploadDurationTotal:   m.uploadDurationTotal,
		DownloadDurationTotal: m.downloadDurationTotal,
		StorageBytes:          m.storageBytes,
		BlobCount:             m.blobCount,
		GCRunsTotal:           m.gcRunsTotal,
		GCBlobsDeleted:        m.gcBlobsDeleted,
		GCLastRunTime:         m.gcLastRunTime,
		GCLastDuration:        m.gcLastDuration,
		UploadErrorCount:      m.uploadErrorCount,
		DownloadErrorCount:    m.downloadErrorCount,
	}
}

// MetricsSnapshot is a point-in-time snapshot of metrics.
type MetricsSnapshot struct {
	UploadBytesTotal      int64
	DownloadBytesTotal    int64
	UploadCount           int64
	DownloadCount         int64
	BlobGetCount          int64
	BlobHeadCount         int64
	BlobPutCount          int64
	ManifestGetCount      int64
	ManifestPutCount      int64
	ManifestDeleteCount   int64
	UploadDurationTotal   int64
	DownloadDurationTotal int64
	StorageBytes          int64
	BlobCount             int64
	GCRunsTotal           int64
	GCBlobsDeleted        int64
	GCLastRunTime         time.Time
	GCLastDuration        time.Duration
	UploadErrorCount      int64
	DownloadErrorCount    int64
}

// FormatPrometheus formats metrics in Prometheus text format.
func (s *MetricsSnapshot) FormatPrometheus() string {
	result := ""

	// Upload/download metrics
	result += "# HELP registry_upload_bytes_total Total bytes uploaded to registry\n"
	result += "# TYPE registry_upload_bytes_total counter\n"
	result += formatMetric("registry_upload_bytes_total", s.UploadBytesTotal)

	result += "# HELP registry_download_bytes_total Total bytes downloaded from registry\n"
	result += "# TYPE registry_download_bytes_total counter\n"
	result += formatMetric("registry_download_bytes_total", s.DownloadBytesTotal)

	result += "# HELP registry_upload_count_total Total number of uploads\n"
	result += "# TYPE registry_upload_count_total counter\n"
	result += formatMetric("registry_upload_count_total", s.UploadCount)

	result += "# HELP registry_download_count_total Total number of downloads\n"
	result += "# TYPE registry_download_count_total counter\n"
	result += formatMetric("registry_download_count_total", s.DownloadCount)

	// Request metrics
	result += "# HELP registry_blob_get_count_total Total blob GET requests\n"
	result += "# TYPE registry_blob_get_count_total counter\n"
	result += formatMetric("registry_blob_get_count_total", s.BlobGetCount)

	result += "# HELP registry_blob_head_count_total Total blob HEAD requests\n"
	result += "# TYPE registry_blob_head_count_total counter\n"
	result += formatMetric("registry_blob_head_count_total", s.BlobHeadCount)

	result += "# HELP registry_blob_put_count_total Total blob PUT requests\n"
	result += "# TYPE registry_blob_put_count_total counter\n"
	result += formatMetric("registry_blob_put_count_total", s.BlobPutCount)

	result += "# HELP registry_manifest_get_count_total Total manifest GET requests\n"
	result += "# TYPE registry_manifest_get_count_total counter\n"
	result += formatMetric("registry_manifest_get_count_total", s.ManifestGetCount)

	result += "# HELP registry_manifest_put_count_total Total manifest PUT requests\n"
	result += "# TYPE registry_manifest_put_count_total counter\n"
	result += formatMetric("registry_manifest_put_count_total", s.ManifestPutCount)

	result += "# HELP registry_manifest_delete_count_total Total manifest DELETE requests\n"
	result += "# TYPE registry_manifest_delete_count_total counter\n"
	result += formatMetric("registry_manifest_delete_count_total", s.ManifestDeleteCount)

	// Duration metrics
	result += "# HELP registry_upload_duration_ms_total Total upload duration in milliseconds\n"
	result += "# TYPE registry_upload_duration_ms_total counter\n"
	result += formatMetric("registry_upload_duration_ms_total", s.UploadDurationTotal)

	result += "# HELP registry_download_duration_ms_total Total download duration in milliseconds\n"
	result += "# TYPE registry_download_duration_ms_total counter\n"
	result += formatMetric("registry_download_duration_ms_total", s.DownloadDurationTotal)

	// Storage metrics
	result += "# HELP registry_storage_bytes Current storage usage in bytes\n"
	result += "# TYPE registry_storage_bytes gauge\n"
	result += formatMetric("registry_storage_bytes", s.StorageBytes)

	result += "# HELP registry_blob_count Current number of blobs stored\n"
	result += "# TYPE registry_blob_count gauge\n"
	result += formatMetric("registry_blob_count", s.BlobCount)

	// GC metrics
	result += "# HELP registry_gc_runs_total Total garbage collection runs\n"
	result += "# TYPE registry_gc_runs_total counter\n"
	result += formatMetric("registry_gc_runs_total", s.GCRunsTotal)

	result += "# HELP registry_gc_blobs_deleted_total Total blobs deleted by GC\n"
	result += "# TYPE registry_gc_blobs_deleted_total counter\n"
	result += formatMetric("registry_gc_blobs_deleted_total", s.GCBlobsDeleted)

	result += "# HELP registry_gc_last_duration_seconds Duration of last GC run in seconds\n"
	result += "# TYPE registry_gc_last_duration_seconds gauge\n"
	result += formatMetricFloat("registry_gc_last_duration_seconds", s.GCLastDuration.Seconds())

	// Error metrics
	result += "# HELP registry_upload_error_count_total Total upload errors\n"
	result += "# TYPE registry_upload_error_count_total counter\n"
	result += formatMetric("registry_upload_error_count_total", s.UploadErrorCount)

	result += "# HELP registry_download_error_count_total Total download errors\n"
	result += "# TYPE registry_download_error_count_total counter\n"
	result += formatMetric("registry_download_error_count_total", s.DownloadErrorCount)

	return result
}

func formatMetric(name string, value int64) string {
	return name + " " + formatInt64(value) + "\n"
}

func formatMetricFloat(name string, value float64) string {
	return name + " " + formatFloat64(value) + "\n"
}

func formatInt64(v int64) string {
	// Simple int64 to string conversion
	if v == 0 {
		return "0"
	}

	negative := v < 0
	if negative {
		v = -v
	}

	digits := make([]byte, 0, 20)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}

	// Reverse digits
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}

	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func formatFloat64(v float64) string {
	// Simple float formatting for Prometheus (good enough for duration metrics)
	// Format as seconds with 3 decimal places (milliseconds precision)
	seconds := int64(v)
	milliseconds := int64((v - float64(seconds)) * 1000)
	return formatInt64(seconds) + "." + formatInt64WithPadding(milliseconds, 3)
}

func formatInt64WithPadding(v int64, minDigits int) string {
	result := formatInt64(v)
	for len(result) < minDigits {
		result = "0" + result
	}
	return result
}
