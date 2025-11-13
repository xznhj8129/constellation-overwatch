package shared

import (
	"time"
)

// API Response types
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Pagination
type PaginationRequest struct {
	Page     int    `json:"page" validate:"min=1"`
	PageSize int    `json:"page_size" validate:"min=1,max=100"`
	OrderBy  string `json:"order_by"`
	Order    string `json:"order" validate:"omitempty,oneof=asc desc"`
}

type PaginationResponse struct {
	Items      interface{} `json:"items"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalItems int64       `json:"total_items"`
	TotalPages int         `json:"total_pages"`
}

// Event types
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Subject   string                 `json:"subject"`
	Data      map[string]interface{} `json:"data"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
}

// Fleet represents a collection of swarms
type Fleet struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	OrgID       string            `json:"org_id"`
	SwarmIDs    []string          `json:"swarm_ids"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Swarm represents a group of entities working together
type Swarm struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	OrgID       string            `json:"org_id"`
	FleetID     string            `json:"fleet_id,omitempty"`
	EntityIDs   []string          `json:"entity_ids"`
	Status      string            `json:"status"`
	Type        string            `json:"type"` // e.g., "formation", "patrol", "search"
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Health check
type HealthStatus struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Version   string            `json:"version,omitempty"`
	Uptime    time.Duration     `json:"uptime,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details,omitempty"`
}

// Constants
const (
	// Entity Types
	EntityTypeVehicle   = "vehicle"
	EntityTypePerson    = "person"
	EntityTypeAsset     = "asset"
	EntityTypeLocation  = "location"
	EntityTypeSensor    = "sensor"
	EntityTypeDevice    = "device"
	
	// Entity Status
	StatusActive   = "active"
	StatusInactive = "inactive"
	StatusUnknown  = "unknown"
	StatusOffline  = "offline"
	StatusOnline   = "online"
	
	// Priority Levels
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
	
	// Organization Types
	OrgTypeCompany    = "company"
	OrgTypeAgency     = "agency"
	OrgTypeIndividual = "individual"
	
	// Event Types
	EventTypeCreated = "created"
	EventTypeUpdated = "updated"
	EventTypeDeleted = "deleted"
	EventTypeStatus  = "status_changed"
	EventTypeAlert   = "alert"
)