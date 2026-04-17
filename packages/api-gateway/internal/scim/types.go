package scim

// Minimal SCIM v2 Core Schemas (RFC 7643) payload types — we implement just
// enough to satisfy Entra's SCIM provisioning client for Phase 2.

const (
	SchemaUser       = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaListResp   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaPatchOp    = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaError      = "urn:ietf:params:scim:api:messages:2.0:Error"
)

type Email struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
	Type    string `json:"type,omitempty"`
}

type Name struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type Meta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
}

type User struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	DisplayName string   `json:"displayName,omitempty"`
	// Active is a pointer so we can distinguish "absent" from "false".
	// When encoding outgoing responses we always set it.
	Active *bool   `json:"active,omitempty"`
	Name   *Name   `json:"name,omitempty"`
	Emails []Email `json:"emails,omitempty"`
	Meta   *Meta   `json:"meta,omitempty"`
}

type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	Resources    []User   `json:"Resources"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
}

type PatchRequest struct {
	Schemas    []string    `json:"schemas"`
	Operations []Operation `json:"Operations"`
}

type Operation struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

type ErrorResponse struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	Detail   string   `json:"detail,omitempty"`
	SCIMType string   `json:"scimType,omitempty"`
}
