package redfish

// ODataIDRef represents a reference to another resource
type ODataIDRef struct {
	ODataID string `json:"@odata.id"`
}

// ServiceRoot represents the Redfish service root
type ServiceRoot struct {
	ODataContext       string           `json:"@odata.context"`
	ODataID            string           `json:"@odata.id"`
	ODataType          string           `json:"@odata.type"`
	ID                 string           `json:"Id"`
	Name               string           `json:"Name"`
	RedfishVersion     string           `json:"RedfishVersion"`
	UUID               string           `json:"UUID"`
	Systems            ODataIDRef       `json:"Systems"`
	Managers           ODataIDRef       `json:"Managers"`
	SessionService     ODataIDRef       `json:"SessionService"`
	AggregationService *ODataIDRef      `json:"AggregationService,omitempty"`
	Links              ServiceRootLinks `json:"Links"`
}

// ServiceRootLinks contains links within the service root
type ServiceRootLinks struct {
	Sessions ODataIDRef `json:"Sessions"`
}

// Collection represents a generic Redfish collection
type Collection struct {
	ODataContext string       `json:"@odata.context"`
	ODataID      string       `json:"@odata.id"`
	ODataType    string       `json:"@odata.type"`
	Name         string       `json:"Name"`
	Members      []ODataIDRef `json:"Members"`
	MembersCount int          `json:"Members@odata.count"`
}

// Session represents a Redfish session
type Session struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	UserName     string `json:"UserName"`
}

// ErrorResponse represents a Redfish error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AggregationService represents the Redfish AggregationService
type AggregationService struct {
	ODataContext       string     `json:"@odata.context"`
	ODataID            string     `json:"@odata.id"`
	ODataType          string     `json:"@odata.type"`
	ID                 string     `json:"Id"`
	Name               string     `json:"Name"`
	AggregationSources ODataIDRef `json:"AggregationSources"`
}

// AggregationSource represents a managed external Redfish service (e.g., a BMC)
// Note: Password is write-only; it is accepted on create/update but never returned.
type AggregationSource struct {
	ODataID     string `json:"@odata.id"`
	ODataType   string `json:"@odata.type"`
	ID          string `json:"Id"`
	Name        string `json:"Name"`
	HostName    string `json:"HostName"`
	UserName    string `json:"UserName"`
	Description string `json:"Description,omitempty"`
	Enabled     bool   `json:"Enabled"`
}
