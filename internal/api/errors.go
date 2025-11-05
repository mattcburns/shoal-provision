/*
Shoal is a Redfish aggregator service.
Copyright (C) 2025  Matthew Burns

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package api

import "net/http"

// NOTE: These helpers and message registry IDs were extracted as part of
// design 019 to centralize error response handling across handlers and middleware.
// During the transition, names are rf*-prefixed to avoid symbol duplication
// with existing helpers in api.go. Call sites can migrate to use these.

var rfValidMessageIDs = map[string]struct{}{
	"Base.1.0.GeneralError":            {},
	"Base.1.0.ResourceNotFound":        {},
	"Base.1.0.MethodNotAllowed":        {},
	"Base.1.0.Unauthorized":            {},
	"Base.1.0.InternalError":           {},
	"Base.1.0.InsufficientPrivilege":   {},
	"Base.1.0.MalformedJSON":           {},
	"Base.1.0.PropertyMissing":         {},
	"Base.1.0.PropertyValueNotInList":  {},
	"Base.1.0.ResourceCannotBeCreated": {},
	"Base.1.0.NotImplemented":          {},
}

// rfWriteErrorResponse writes a Redfish-compliant error payload with ExtendedInfo.
// It also applies WWW-Authenticate for 401 responses and relies on the shared
// JSON responder to set headers consistently.
func rfWriteErrorResponse(w http.ResponseWriter, status int, code, message string) {
	// Set WWW-Authenticate header for 401 responses
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="Redfish"`)
	}

	// Map our code to a Base registry MessageId (best-effort)
	messageID := "Base.1.0.GeneralError"
	if _, ok := rfValidMessageIDs[code]; ok {
		messageID = code
	}

	errorResp := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
			"@Message.ExtendedInfo": []map[string]interface{}{
				{
					"@odata.type": "#Message.v1_1_0.Message",
					"MessageId":   messageID,
					"Message":     message,
					"Severity":    rfSeverityForStatus(status),
					"Resolution":  rfResolutionForMessageID(messageID),
				},
			},
		},
	}

	// Use centralized JSON response helper to ensure headers are uniform.
	rfWriteJSONResponse(w, status, errorResp)
}

// rfSeverityForStatus maps HTTP status codes to Redfish severity strings.
func rfSeverityForStatus(status int) string {
	switch {
	case status >= 500:
		return "Critical"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "Critical"
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusBadRequest || status == http.StatusConflict:
		return "Warning"
	default:
		return "OK"
	}
}

// rfResolutionForMessageID returns a generic resolution string for known Base registry messages.
func rfResolutionForMessageID(msgID string) string {
	switch msgID {
	case "Base.1.0.ResourceNotFound":
		return "Provide a valid resource identifier and resubmit the request."
	case "Base.1.0.MethodNotAllowed":
		return "Use an allowed HTTP method for the target resource and resubmit the request."
	case "Base.1.0.Unauthorized":
		return "Provide valid credentials and resubmit the request."
	case "Base.1.0.InsufficientPrivilege":
		return "Resubmit the request using an account with the required privileges."
	case "Base.1.0.MalformedJSON":
		return "Correct the JSON payload formatting and resubmit the request."
	case "Base.1.0.PropertyMissing":
		return "Include all required properties in the request and resubmit."
	case "Base.1.0.PropertyValueNotInList":
		return "Use a supported value for the property and resubmit the request."
	case "Base.1.0.ResourceCannotBeCreated":
		return "Verify the request data and permissions, correct any issues, and resubmit."
	case "Base.1.0.NotImplemented":
		return "Remove the unsupported operation from the request or await a future implementation."
	case "Base.1.0.InternalError":
		fallthrough
	default:
		return "Retry the operation; if the problem persists, contact the service provider."
	}
}
