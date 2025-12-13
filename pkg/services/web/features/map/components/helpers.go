package components

import (
	"fmt"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
)

// formatCoord formats a coordinate value with 6 decimal places
func formatCoord(coord float64) string {
	return fmt.Sprintf("%.6f", coord)
}

// formatCoordShort formats lat/lng as a short string
func formatCoordShort(lat, lng float64) string {
	return fmt.Sprintf("%.4f, %.4f", lat, lng)
}

// formatAltitude formats altitude with meters suffix
func formatAltitude(alt float64) string {
	return fmt.Sprintf("%.1fm", alt)
}

// formatHeading formats heading in degrees
func formatHeading(heading int16) string {
	return fmt.Sprintf("%d°", heading)
}

// formatSpeed formats speed in m/s
func formatSpeed(speed float64) string {
	return fmt.Sprintf("%.1f m/s", speed)
}

// getEntityDisplayName returns the display name for an entity
func getEntityDisplayName(entity shared.EntityState) string {
	if entity.Name != "" {
		return entity.Name
	}
	if len(entity.EntityID) > 8 {
		return entity.EntityID[:8] + "..."
	}
	return entity.EntityID
}
