package services

import (
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/google/uuid"
)

type OrganizationService struct {
	db   *sql.DB
	nats *embeddednats.EmbeddedNATS
}

func (s *OrganizationService) DB() *sql.DB {
	return s.db
}

func NewOrganizationService(db *sql.DB, nats *embeddednats.EmbeddedNATS) *OrganizationService {
	return &OrganizationService{
		db:   db,
		nats: nats,
	}
}

func (s *OrganizationService) CreateOrganization(req *ontology.CreateOrganizationRequest) (*ontology.Organization, error) {
	orgID := uuid.New().String()
	now := time.Now()

	metadataJSON := "{}"
	if req.Metadata != nil {
		bytes, _ := json.Marshal(req.Metadata)
		metadataJSON = string(bytes)
	}

	_, err := s.db.Exec(
		`INSERT INTO organizations (org_id, name, org_type, description, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		orgID, req.Name, req.OrgType, req.Description, metadataJSON, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}

	return &ontology.Organization{
		OrgID:       orgID,
		Name:        req.Name,
		OrgType:     req.OrgType,
		Description: req.Description,
		Metadata:    metadataJSON,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *OrganizationService) ListOrganizations() ([]ontology.Organization, error) {
	rows, err := s.db.Query(
		`SELECT org_id, name, org_type, description, metadata, created_at, updated_at FROM organizations`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query organizations: %w", err)
	}
	defer rows.Close()

	var orgs []ontology.Organization
	for rows.Next() {
		var org ontology.Organization
		var createdAt, updatedAt string

		err := rows.Scan(&org.OrgID, &org.Name, &org.OrgType, &org.Description, &org.Metadata, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan organization: %w", err)
		}

		org.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		org.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		orgs = append(orgs, org)
	}

	return orgs, nil
}

func (s *OrganizationService) GetOrganization(orgID string) (*ontology.Organization, error) {
	var org ontology.Organization
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT org_id, name, org_type, description, metadata, created_at, updated_at
		 FROM organizations WHERE org_id = ?`,
		orgID,
	).Scan(&org.OrgID, &org.Name, &org.OrgType, &org.Description, &org.Metadata, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("organization not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query organization: %w", err)
	}

	org.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	org.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &org, nil
}

func (s *OrganizationService) UpdateOrganization(orgID string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// Build dynamic update query
	query := "UPDATE organizations SET updated_at = ? "
	args := []interface{}{time.Now().Format(time.RFC3339)}

	for key, value := range updates {
		switch key {
		case "name", "org_type", "description":
			query += fmt.Sprintf(", %s = ?", key)
			args = append(args, value)
		case "metadata":
			bytes, _ := json.Marshal(value)
			query += ", metadata = ?"
			args = append(args, string(bytes))
		}
	}

	query += " WHERE org_id = ?"
	args = append(args, orgID)

	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update organization: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("organization not found")
	}

	// If name was updated, we need to update all entities in KV with the new org name
	if _, ok := updates["name"]; ok {
		go s.syncOrgNameToKV(orgID, updates["name"].(string))
	}

	return nil
}

// syncOrgNameToKV updates the OrgName in all KV entries for this organization
func (s *OrganizationService) syncOrgNameToKV(orgID, newName string) {
	if s.nats == nil {
		return
	}

	kv := s.nats.KeyValue()
	if kv == nil {
		return
	}

	// We need to find all entities for this org.
	// Query DB for entity IDs
	rows, err := s.db.Query("SELECT entity_id FROM entities WHERE org_id = ?", orgID)
	if err != nil {
		logger.Errorw("Failed to query entities for org name sync", "org_id", orgID, "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			continue
		}

		key := shared.EntityKey(entityID)
		entry, err := kv.Get(key)
		if err != nil {
			continue
		}

		var state shared.EntityState
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			continue
		}

		// Update OrgName
		state.OrgName = newName
		state.UpdatedAt = time.Now()

		data, err := json.Marshal(state)
		if err != nil {
			continue
		}

		if _, err := kv.Put(key, data); err != nil {
			logger.Errorw("Failed to update org name in KV for entity", "entity_id", entityID, "error", err)
		}
	}

	logger.Infow("Synced organization name to KV entities", "org_id", orgID, "new_name", newName)
}

func (s *OrganizationService) DeleteOrganization(orgID string) error {
	result, err := s.db.Exec("DELETE FROM organizations WHERE org_id = ?", orgID)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("organization not found")
	}

	return nil
}
