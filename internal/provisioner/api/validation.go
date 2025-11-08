package api

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

// NOTE:
// This file implements a small validation wrapper for Provisioner recipes (022).
// It loads an embedded JSON Schema document, and performs a pragmatic subset
// of validation consistent with the schema. A full draft-07 JSON Schema engine
// is intentionally avoided to keep dependencies minimal; replace the internal
// checks with a standards-compliant validator when policy allows adding one.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"shoal/internal/provisioner/schema"
	"strings"
)

// Embed the canonical recipe schema for visibility and future full validation.
//
// Schema bytes are provided by the schema package.

// ValidationError describes a single field-level validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validator validates a recipe against the embedded schema.
type Validator interface {
	// ValidateRecipe returns a slice of validation errors.
	// If the slice is non-empty, callers should consider the input invalid (HTTP 400).
	// The returned error is reserved for unexpected system failures (e.g., JSON parse error).
	ValidateRecipe(raw json.RawMessage) ([]ValidationError, error)
	// Schema returns the JSON Schema bytes used by the validator (for debugging / introspection).
	Schema() []byte
}

// DefaultValidator implements Validator with pragmatic checks aligned to the schema.
type DefaultValidator struct {
	schema []byte
}

// NewDefaultValidator constructs a validator using the embedded schema.
func NewDefaultValidator() *DefaultValidator {
	return &DefaultValidator{schema: schema.Recipe()}
}

// Schema returns the embedded schema bytes.
func (v *DefaultValidator) Schema() []byte { return append([]byte(nil), v.schema...) }

// ValidateRecipe performs minimal, deterministic checks mirroring the schema's key constraints.
// This is not a full JSON Schema implementation; it focuses on correctness for critical fields
// and provides structured errors for clients.
func (v *DefaultValidator) ValidateRecipe(raw json.RawMessage) ([]ValidationError, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []ValidationError{{Field: "(root)", Message: "recipe must be a non-empty JSON object"}}, nil
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse recipe: %w", err)
	}
	errs := make([]ValidationError, 0, 8)

	// Reject non-object
	if obj == nil {
		errs = append(errs, ValidationError{Field: "(root)", Message: "recipe must be a JSON object"})
		return errs, nil
	}

	// additionalProperties: false (flag unknown top-level keys)
	known := map[string]struct{}{
		"schema_version":   {},
		"task_target":      {},
		"target_disk":      {},
		"oci_url":          {},
		"firmware_url":     {},
		"partition_layout": {},
		"user_data":        {},
		"unattend_xml":     {},
		"wim_index":        {},
		"ks.cfg":           {},
		"env":              {},
		"notes":            {},
	}
	for k := range obj {
		if _, ok := known[k]; !ok {
			errs = append(errs, ValidationError{Field: k, Message: "unknown field (additionalProperties is not allowed)"})
		}
	}

	// Required: task_target, target_disk
	taskTarget, ok := obj["task_target"]
	if !ok {
		errs = append(errs, ValidationError{Field: "task_target", Message: "is required"})
	} else if s, ok := taskTarget.(string); !ok || strings.TrimSpace(s) == "" {
		errs = append(errs, ValidationError{Field: "task_target", Message: "must be a non-empty string"})
	} else if !reTargetSuffix.MatchString(s) {
		errs = append(errs, ValidationError{Field: "task_target", Message: "must match pattern .*\\.target (e.g., install-linux.target)"})
	}

	targetDisk, ok := obj["target_disk"]
	if !ok {
		errs = append(errs, ValidationError{Field: "target_disk", Message: "is required"})
	} else if s, ok := targetDisk.(string); !ok || strings.TrimSpace(s) == "" {
		errs = append(errs, ValidationError{Field: "target_disk", Message: "must be a non-empty string"})
	}

	// Optional: schema_version
	if v, ok := obj["schema_version"]; ok {
		if s, ok := v.(string); !ok {
			errs = append(errs, ValidationError{Field: "schema_version", Message: "must be a string"})
		} else if !reSchemaVersion.MatchString(s) {
			errs = append(errs, ValidationError{Field: "schema_version", Message: "must match ^1(\\.[0-9]+)?$"})
		}
	}

	// Optional: oci_url
	if v, ok := obj["oci_url"]; ok {
		if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
			errs = append(errs, ValidationError{Field: "oci_url", Message: "must be a non-empty string"})
		} else if !reOCIRef.MatchString(s) {
			errs = append(errs, ValidationError{Field: "oci_url", Message: "does not look like a valid OCI reference (controller:port/repo[:tag|@digest])"})
		}
	}

	// Optional: wim_index (integer, >= 1)
	if v, ok := obj["wim_index"]; ok {
		if num, ok := v.(float64); !ok {
			errs = append(errs, ValidationError{Field: "wim_index", Message: "must be an integer"})
		} else if num < 1 {
			errs = append(errs, ValidationError{Field: "wim_index", Message: "must be >= 1 (minimum: 1)"})
		} else if num != float64(int(num)) {
			errs = append(errs, ValidationError{Field: "wim_index", Message: "must be an integer (no fractional part)"})
		}
	}

	// Optional: firmware_url (basic string check; RFC URI validation omitted)
	if v, ok := obj["firmware_url"]; ok {
		if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
			errs = append(errs, ValidationError{Field: "firmware_url", Message: "must be a non-empty string URL"})
		}
	}

	// Optional: partition_layout
	if v, ok := obj["partition_layout"]; ok {
		arr, ok := v.([]any)
		if !ok {
			errs = append(errs, ValidationError{Field: "partition_layout", Message: "must be an array"})
		} else if len(arr) == 0 {
			errs = append(errs, ValidationError{Field: "partition_layout", Message: "must have at least one item"})
		} else {
			for i, it := range arr {
				mp, ok := it.(map[string]any)
				if !ok {
					errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d]", i), Message: "must be an object"})
					continue
				}
				// size (required)
				sz, ok := mp["size"]
				if !ok {
					errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].size", i), Message: "is required"})
				} else if s, ok := sz.(string); !ok || strings.TrimSpace(s) == "" {
					errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].size", i), Message: "must be a non-empty string"})
				} else if !rePartSize.MatchString(s) && s != "100%" {
					errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].size", i), Message: "invalid size format (examples: 512M, 1G, 100%)"})
				}
				// type_guid (optional)
				if tg, ok := mp["type_guid"]; ok {
					if s, ok := tg.(string); !ok {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].type_guid", i), Message: "must be a string"})
					} else if !reTypeGUID.MatchString(s) {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].type_guid", i), Message: "must be a hex code (e.g., ef00) or GUID"})
					}
				}
				// format (optional)
				if f, ok := mp["format"]; ok {
					if s, ok := f.(string); !ok {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].format", i), Message: "must be a string"})
					} else if !allowedFSFormat[s] {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].format", i), Message: "must be one of vfat, ext4, xfs, ntfs, swap, raw, none"})
					}
				}
				// label (optional)
				if l, ok := mp["label"]; ok {
					if s, ok := l.(string); !ok {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].label", i), Message: "must be a string"})
					} else if len(s) > 32 {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].label", i), Message: "length must be <= 32"})
					}
				}
				// mountpoint (optional)
				if m, ok := mp["mountpoint"]; ok {
					if s, ok := m.(string); !ok || strings.TrimSpace(s) == "" {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].mountpoint", i), Message: "must be a non-empty string"})
					}
				}
				// bootable (optional)
				if b, ok := mp["bootable"]; ok {
					if _, ok := b.(bool); !ok {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].bootable", i), Message: "must be a boolean"})
					}
				}
				// additionalProperties: false (best-effort)
				for k := range mp {
					if !knownPartProps[k] {
						errs = append(errs, ValidationError{Field: fmt.Sprintf("partition_layout[%d].%s", i, k), Message: "unknown field"})
					}
				}
			}
		}
	}

	// Optional payloads
	for field := range map[string]struct{}{
		"user_data":    {},
		"unattend_xml": {},
		"ks.cfg":       {},
	} {
		if v, ok := obj[field]; ok {
			if e := validatePayload(field, v); e != nil {
				errs = append(errs, *e)
			}
		}
	}

	// env (map[string]string)
	if v, ok := obj["env"]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			errs = append(errs, ValidationError{Field: "env", Message: "must be an object of string values"})
		} else {
			for k, vv := range mp {
				if _, ok := vv.(string); !ok {
					errs = append(errs, ValidationError{Field: fmt.Sprintf("env.%s", k), Message: "must be a string"})
				}
			}
		}
	}

	// notes (string, <=2000)
	if v, ok := obj["notes"]; ok {
		if s, ok := v.(string); !ok {
			errs = append(errs, ValidationError{Field: "notes", Message: "must be a string"})
		} else if len(s) > 2000 {
			errs = append(errs, ValidationError{Field: "notes", Message: "length must be <= 2000"})
		}
	}

	return errs, nil
}

// ValidateRecipe is a convenience entrypoint using the default validator.
func ValidateRecipe(raw json.RawMessage) ([]ValidationError, error) {
	return NewDefaultValidator().ValidateRecipe(raw)
}

// validatePayload enforces the oneOf(payload) shape: string | {content} | {url} | {path}
func validatePayload(field string, v any) *ValidationError {
	switch t := v.(type) {
	case string:
		// ok: inline string
		return nil
	case map[string]any:
		has := map[string]bool{
			"content": false,
			"url":     false,
			"path":    false,
		}
		for k := range t {
			if _, ok := has[k]; !ok {
				msg := "unknown key; allowed keys are content, url, path"
				return &ValidationError{Field: field, Message: msg}
			}
			has[k] = true
		}
		switch {
		case has["content"]:
			if s, ok := t["content"].(string); !ok || strings.TrimSpace(s) == "" {
				return &ValidationError{Field: field + ".content", Message: "must be a non-empty string"}
			}
		case has["url"]:
			if s, ok := t["url"].(string); !ok || strings.TrimSpace(s) == "" {
				return &ValidationError{Field: field + ".url", Message: "must be a non-empty string URL"}
			}
		case has["path"]:
			if s, ok := t["path"].(string); !ok || strings.TrimSpace(s) == "" {
				return &ValidationError{Field: field + ".path", Message: "must be a non-empty string path"}
			}
		default:
			return &ValidationError{Field: field, Message: "must provide one of content, url, or path"}
		}
	default:
		return &ValidationError{Field: field, Message: "must be a string or object"}
	}
	return nil
}

// Compile-time regexps and constants

var (
	reTargetSuffix  = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.target$`)
	reSchemaVersion = regexp.MustCompile(`^1(\.[0-9]+)?$`)
	// Basic OCI reference heuristic (host[:port]/repo[/sub]:tag or @digest).
	reOCIRef = regexp.MustCompile(`^[A-Za-z0-9._:-]+(/[A-Za-z0-9._-]+)*(?:@[A-Za-z0-9:+\-=._/]+|:[A-Za-z0-9._-]+)?$`)
	// Partition size: allow 100% or NN with unit suffix; kept pragmatic.
	rePartSize = regexp.MustCompile(`^[1-9][0-9]*([KMGTP]i?B?|[KMGTP]|M|G)$`)
	// GPT type GUID or short hex (e.g., ef00)
	reTypeGUID = regexp.MustCompile(`^([0-9A-Fa-f]{4}|[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12})$`)
)

var allowedFSFormat = map[string]bool{
	"vfat": true,
	"ext4": true,
	"xfs":  true,
	"ntfs": true,
	"swap": true,
	"raw":  true,
	"none": true,
}

var knownPartProps = map[string]bool{
	"size":       true,
	"type_guid":  true,
	"format":     true,
	"label":      true,
	"mountpoint": true,
	"bootable":   true,
}

// Helper to surface whether the validator has a schema embedded.
// A missing schema is treated as a system misconfiguration.
func (v *DefaultValidator) ensureSchemaPresent() error {
	if len(v.schema) == 0 {
		return errors.New("recipe schema not embedded")
	}
	return nil
}
