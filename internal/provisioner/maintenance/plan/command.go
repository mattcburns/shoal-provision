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

// Package plan provides common command planning helpers for maintenance OS
// workflows.
package plan

import "strings"

// Command represents an executable program with arguments and a human-readable
// description.
type Command struct {
	Program     string
	Args        []string
	Description string
}

// Shell renders the command as a shell-ready string by quoting arguments.
func (c Command) Shell() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Program)
	for _, arg := range c.Args {
		parts = append(parts, Quote(arg))
	}
	return strings.Join(parts, " ")
}

// Quote returns arg surrounded by single quotes if it contains shell metacharacters.
func Quote(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\'', '"', '$', '`', '\\', '|', '&', ';', '<', '>', '(', ')':
			return true
		default:
			return false
		}
	}) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
