package ontology

import (
	"time"
)

type Organization struct {
	OrgID       string    `json:"org_id" db:"org_id"`
	Name        string    `json:"name" db:"name"`
	OrgType     string    `json:"org_type" db:"org_type"`
	Description string    `json:"description,omitempty" db:"description"`
	Metadata    string    `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type CreateOrganizationRequest struct {
	Name        string                 `json:"name" minLength:"1" maxLength:"255" doc:"Organization name"`
	OrgType     string                 `json:"org_type" enum:"military,civilian,commercial,ngo" doc:"Organization type"`
	Description string                 `json:"description,omitempty" maxLength:"500" doc:"Organization description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty" doc:"Arbitrary metadata"`
}
