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

package partition

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"shoal/internal/provisioner/maintenance/plan"
)

// LayoutEntry mirrors the recipe partition schema described in
// design/029_Workflow_Linux.md.
type LayoutEntry struct {
	Size     string `json:"size"`
	TypeGUID string `json:"type_guid"`
	Format   string `json:"format"`
	Label    string `json:"label"`
}

// Plan produces an ordered list of partitioning commands using sgdisk and
// mkfs/mkswap, based on the declarative layout specification.
func Plan(disk string, layoutJSON []byte) ([]plan.Command, error) {
	if disk == "" {
		return nil, errors.New("partition: disk required")
	}
	var entries []LayoutEntry
	if err := json.Unmarshal(layoutJSON, &entries); err != nil {
		return nil, fmt.Errorf("partition: decode layout: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("partition: layout is empty")
	}

	cmds := []plan.Command{
		{Program: "sgdisk", Args: []string{"--zap-all", disk}, Description: "wipe existing partition table"},
		{Program: "sgdisk", Args: []string{"-o", disk}, Description: "initialize new GPT"},
	}

	for idx, entry := range entries {
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("partition %d: %w", idx+1, err)
		}
		typeGUID, err := normalizeTypeGUID(entry.TypeGUID)
		if err != nil {
			return nil, fmt.Errorf("partition %d: %w", idx+1, err)
		}

		partNum := idx + 1
		sizeToken := strings.TrimSpace(entry.Size)
		var end string
		if strings.EqualFold(sizeToken, "100%") {
			end = "0"
		} else {
			sizeToken = strings.TrimPrefix(sizeToken, "+")
			end = "+" + sizeToken
		}
		cmds = append(cmds, plan.Command{
			Program:     "sgdisk",
			Args:        []string{"-n", fmt.Sprintf("%d:0:%s", partNum, end), disk},
			Description: fmt.Sprintf("create partition %d", partNum),
		})
		cmds = append(cmds, plan.Command{
			Program:     "sgdisk",
			Args:        []string{"-t", fmt.Sprintf("%d:%s", partNum, typeGUID), disk},
			Description: fmt.Sprintf("set partition %d type", partNum),
		})
		if entry.Label != "" {
			cmds = append(cmds, plan.Command{
				Program:     "sgdisk",
				Args:        []string{"-c", fmt.Sprintf("%d:%s", partNum, entry.Label), disk},
				Description: fmt.Sprintf("label partition %d", partNum),
			})
		}

		formatCmds, err := formatCommands(entry.Format, entry.Label, partitionDeviceName(disk, partNum))
		if err != nil {
			return nil, fmt.Errorf("partition %d: %w", partNum, err)
		}
		cmds = append(cmds, formatCmds...)
	}

	cmds = append(cmds, plan.Command{
		Program:     "sgdisk",
		Args:        []string{"-p", disk},
		Description: "print resulting partition table",
	})

	return cmds, nil
}

func validateEntry(e LayoutEntry) error {
	size := strings.TrimSpace(e.Size)
	if size == "" {
		return errors.New("size must be specified")
	}
	size = strings.TrimPrefix(size, "+")
	if size == "" {
		return errors.New("size token is empty")
	}
	if strings.TrimSpace(e.TypeGUID) == "" {
		return errors.New("type_guid must be specified")
	}
	if e.Label != "" {
		if len(e.Label) > 36 {
			return errors.New("label exceeds 36 characters")
		}
		if strings.ContainsRune(e.Label, ':') {
			return errors.New("label may not contain colon characters")
		}
	}
	if e.Format == "" {
		return nil
	}
	switch strings.ToLower(e.Format) {
	case "vfat", "ext4", "xfs", "btrfs", "swap", "raw":
		return nil
	default:
		return fmt.Errorf("unsupported format %q", e.Format)
	}
}

func normalizeTypeGUID(input string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return "", errors.New("type_guid is empty")
	}
	if full, ok := typeAlias[s]; ok {
		return strings.ToUpper(full), nil
	}
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return strings.ToUpper(s), nil
	}
	return "", fmt.Errorf("unknown type GUID alias %q", input)
}

var typeAlias = map[string]string{
	"ef00": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", // EFI system partition
	"8300": "0FC63DAF-8483-4772-8E79-3D69D8477DE4", // Linux filesystem
	"8200": "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F", // Linux swap
	"0700": "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7", // Basic data
	"0c01": "F4019732-066E-4E12-8273-346C5641494F", // Microsoft basic data (alternative)
}

func partitionDeviceName(disk string, partNum int) string {
	if len(disk) > 0 {
		last := disk[len(disk)-1]
		if last >= '0' && last <= '9' {
			return fmt.Sprintf("%sp%d", disk, partNum)
		}
	}
	return fmt.Sprintf("%s%d", disk, partNum)
}

func formatCommands(format, label, device string) ([]plan.Command, error) {
	if format == "" || strings.EqualFold(format, "raw") {
		return nil, nil
	}
	switch strings.ToLower(format) {
	case "vfat":
		args := []string{"-F", "32"}
		if label != "" {
			args = append(args, "-n", label)
		}
		args = append(args, device)
		return []plan.Command{{
			Program:     "mkfs.vfat",
			Args:        args,
			Description: fmt.Sprintf("create FAT filesystem on %s", device),
		}}, nil
	case "ext4":
		args := []string{"-F"}
		if label != "" {
			args = append(args, "-L", label)
		}
		args = append(args, device)
		return []plan.Command{{
			Program:     "mkfs.ext4",
			Args:        args,
			Description: fmt.Sprintf("create ext4 filesystem on %s", device),
		}}, nil
	case "xfs":
		args := []string{"-f"}
		if label != "" {
			args = append(args, "-L", label)
		}
		args = append(args, device)
		return []plan.Command{{
			Program:     "mkfs.xfs",
			Args:        args,
			Description: fmt.Sprintf("create XFS filesystem on %s", device),
		}}, nil
	case "btrfs":
		args := []string{"-f"}
		if label != "" {
			args = append(args, "-L", label)
		}
		args = append(args, device)
		return []plan.Command{{
			Program:     "mkfs.btrfs",
			Args:        args,
			Description: fmt.Sprintf("create Btrfs filesystem on %s", device),
		}}, nil
	case "swap":
		args := []string{}
		if label != "" {
			args = append(args, "-L", label)
		}
		args = append(args, device)
		return []plan.Command{{
			Program:     "mkswap",
			Args:        args,
			Description: fmt.Sprintf("create swap area on %s", device),
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}
