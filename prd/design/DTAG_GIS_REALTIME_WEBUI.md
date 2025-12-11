# DTAG GIS Real-time Web UI: Technical Research & Architecture Design

## Executive Summary

This document outlines the technical architecture for extending Constellation Overwatch's web service to support real-time GIS mapping with offline-first capabilities. The proposed system leverages the existing NATS KV store `CONSTELLATION_GLOBAL_STATE`, SSE streaming infrastructure, and modern web mapping technologies to provide tactical-grade real-time situational awareness.

## Current System Analysis

### Existing Web Service Architecture

**Technology Stack:**
- **Go Templ**: Server-side templating with compile-time safety
- **Datastar**: Hypermedia-driven SSE framework for real-time UI updates  
- **NATS JetStream**: Event streaming and KV store for global state
- **Session-based Authentication**: Secure access control

**Key Strengths:**
1. **Real-time SSE Infrastructure**: Proven `/api/overwatch/kv/watch` endpoint delivering sub-second entity updates
2. **Scalable State Management**: NATS KV provides atomic updates and watch capabilities
3. **Modular Component Architecture**: Templ components enable reusable UI elements
4. **High-Frequency Data Handling**: Existing system processes 50Hz attitude data efficiently

### NATS KV Global State Structure

**KV Bucket:** `CONSTELLATION_GLOBAL_STATE`
- **Key Pattern:** `entity:{entity_id}` (e.g., `drone-001`, `sensor-alpha-1`)
- **Value:** Comprehensive JSON EntityState with position, attitude, power, analytics
- **Update Rate:** 1-50 Hz depending on telemetry type
- **Position Data**: Both global GPS (`latitude`, `longitude`, `altitude_msl`) and local NED coordinates

**Position State Schema:**
```json
{
  "position": {
    "global": {
      "latitude": 37.0746863,
      "longitude": 126.8984223,
      "altitude_msl": 194.45886,
      "timestamp": "2025-11-18T22:27:27.959220Z"
    },
    "local": {
      "x": -0.027861081,
      "y": 0.02613387,
      "z": -13.482539,
      "timestamp": "2025-11-18T22:27:27.980889Z"
    }
  }
}
```

### MAVLink Position Data Flow

**Data Sources:**
- `GPS_RAW_INT`: Global position (1-5 Hz)
- `LOCAL_POSITION_NED`: Local coordinates (10-50 Hz)  
- `ALTITUDE`: Altitude references (1-5 Hz)
- `VFR_HUD`: Ground speed and heading (4 Hz)

**Processing Pipeline:**
1. MAVLink messages → `constellation.telemetry.{org_id}.{entity_id}`
2. Telemetry Worker parses and aggregates state
3. Atomic KV store update: `entity:{entity_id}`
4. SSE broadcast to web clients via `/api/overwatch/kv/watch`

## Proposed Real-time GIS Architecture

### Core Design Principles

1. **Offline-First**: Map functionality without network dependency
2. **Real-time**: Sub-second entity position updates
3. **Scalable**: Support 1000+ simultaneous entities  
4. **Tactical**: Military grid systems (MGRS) and coordinate projections
5. **Modular**: Web components for reusability and maintainability

### Technology Selection

**Mapping Library:** **Leaflet + OpenLayers Hybrid**
- **Leaflet**: Primary library for broad compatibility and plugin ecosystem
- **OpenLayers**: Advanced GIS features (MGRS, projections) when needed
- **Rationale**: Balance between simplicity and capability for tactical applications

**Tile Strategy:** **PMTiles + Vector Tiles**
- **PMTiles**: Cloud-optimized single-file archives for offline capability
- **Vector tiles**: 70% size reduction, infinite scalability, dynamic styling  
- **Fallback**: Raster tiles for older devices

**Coordinate Systems:** **mgrs-js Library**
- **NGA-official**: Military Grid Reference System implementation
- **NATO-compliant**: Standard military coordinate conversions
- **Precision**: Grid zone to meter-level accuracy

### Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     <tactical-map>                         │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  │ <map-overlay>   │  │ <entity-layer>  │  │ <control-panel> │
│  │ - Grid Systems  │  │ - Real-time pos │  │ - Layer toggles │
│  │ - Coordinate    │  │ - Icons/tracks  │  │ - Projections   │
│  │ - Measurement   │  │ - Info popups   │  │ - Tools         │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐
│  │              <sse-position-stream>                      │
│  │  - NATS KV watch connection                            │
│  │  - 60fps position updates                              │
│  │  - Connection management & reconnect                   │
│  └─────────────────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────┘
```

### Web Components Implementation

**1. `<tactical-map>` - Main Map Container**
```javascript
class TacticalMap extends HTMLElement {
    constructor() {
        super();
        this.attachShadow({ mode: 'open' });
        this.map = null;
        this.entities = new Map();
        this.positionStream = null;
    }
    
    connectedCallback() {
        this.initializeMap();
        this.setupPositionStream();
    }
    
    initializeMap() {
        // Initialize Leaflet with PMTiles support
        // Configure offline tile sources
        // Setup MGRS overlay
    }
    
    setupPositionStream() {
        this.positionStream = new SSEPositionStream(
            '/api/overwatch/kv/watch',
            this.updateEntityPositions.bind(this)
        );
    }
}
```

**2. `<entity-layer>` - Real-time Entity Management**
```javascript
class EntityLayer extends HTMLElement {
    updateEntity(entityState) {
        const entity = this.entities.get(entityState.entity_id);
        
        if (!entity) {
            // Create new entity marker
            this.createEntityMarker(entityState);
        } else {
            // Update existing position with smooth animation
            this.animateToPosition(entity, entityState.position.global);
        }
    }
    
    createEntityMarker(entityState) {
        const marker = L.marker([
            entityState.position.global.latitude,
            entityState.position.global.longitude
        ], {
            icon: this.getEntityIcon(entityState.entity_type),
            rotationAngle: this.getHeading(entityState)
        });
        
        // Add entity info popup
        marker.bindPopup(this.createEntityPopup(entityState));
        
        this.entities.set(entityState.entity_id, marker);
        marker.addTo(this.map);
    }
}
```

**3. `<sse-position-stream>` - Real-time Data Management**
```javascript
class SSEPositionStream {
    constructor(endpoint, updateCallback) {
        this.endpoint = endpoint;
        this.updateCallback = updateCallback;
        this.eventSource = null;
        this.reconnectDelay = 1000;
        this.maxReconnectDelay = 30000;
    }
    
    connect() {
        this.eventSource = new EventSource(this.endpoint);
        
        this.eventSource.onmessage = (event) => {
            const data = JSON.parse(event.data);
            this.processEntityUpdate(data);
        };
        
        this.eventSource.onerror = () => {
            this.scheduleReconnect();
        };
    }
    
    processEntityUpdate(data) {
        // Parse Datastar patch signals
        if (data.entityStatesByOrg) {
            Object.values(data.entityStatesByOrg).forEach(orgEntities => {
                Object.values(orgEntities).forEach(entity => {
                    if (entity.position?.global) {
                        this.updateCallback(entity);
                    }
                });
            });
        }
    }
}
```

### Map Integration with Existing Overwatch Template

**Enhanced Overwatch Template Structure:**
```go
// pkg/services/web/templates/overwatch.templ
templ OverwatchMapPage() {
    @Layout("Tactical Map") {
        @Navigation("overwatch") {
            <div class="map-container">
                <!-- Existing debug panel -->
                @OverwatchDebugPanel()
                
                <!-- New tactical map component -->
                <tactical-map 
                    id="tactical-map"
                    data-signals="{
                        mapCenter: [37.0746863, 126.8984223],
                        zoomLevel: 15,
                        projection: 'EPSG:4326',
                        gridSystem: 'MGRS',
                        entityStates: {}
                    }"
                    data-init="@get('/api/overwatch/map/init')">
                    
                    <!-- Entity layer for drone positions -->
                    <entity-layer 
                        data-entity-types="aircraft_multirotor,ground_vehicle,sensor_fixed">
                    </entity-layer>
                    
                    <!-- Map overlays -->
                    <map-overlay data-type="mgrs-grid"></map-overlay>
                    <map-overlay data-type="coordinate-display"></map-overlay>
                    
                    <!-- Control panel -->
                    <control-panel>
                        <layer-toggle data-layer="entities">Entities</layer-toggle>
                        <layer-toggle data-layer="mgrs">MGRS Grid</layer-toggle>
                        <projection-selector></projection-selector>
                    </control-panel>
                </tactical-map>
            </div>
        }
    }
}
```

### Real-time Position Update Pipeline

**SSE Message Flow:**
1. **NATS KV Change**: Entity position updated in `CONSTELLATION_GLOBAL_STATE`
2. **Overwatch Handler**: Detects change via KV watcher
3. **SSE Broadcast**: Sends Datastar signals with position data
4. **Client Processing**: Web component updates map markers
5. **Smooth Animation**: Interpolated movement for 60fps updates

**Performance Optimizations:**
```javascript
class HighFrequencyPositionManager {
    constructor() {
        this.updateBuffer = new Map();
        this.animationFrame = null;
    }
    
    // Buffer rapid position updates
    queuePositionUpdate(entityId, position) {
        this.updateBuffer.set(entityId, position);
        
        if (!this.animationFrame) {
            this.animationFrame = requestAnimationFrame(() => {
                this.flushUpdates();
            });
        }
    }
    
    // Batch process at 60fps
    flushUpdates() {
        this.updateBuffer.forEach((position, entityId) => {
            this.animateEntityToPosition(entityId, position);
        });
        
        this.updateBuffer.clear();
        this.animationFrame = null;
    }
    
    // Smooth interpolation for position updates
    animateEntityToPosition(entityId, targetPosition) {
        const entity = this.entities.get(entityId);
        const currentPos = entity.getLatLng();
        
        // Calculate interpolation steps for smooth movement
        const steps = 10;
        const latStep = (targetPosition.lat - currentPos.lat) / steps;
        const lngStep = (targetPosition.lng - currentPos.lng) / steps;
        
        let step = 0;
        const animate = () => {
            if (step < steps) {
                const newPos = L.latLng(
                    currentPos.lat + (latStep * step),
                    currentPos.lng + (lngStep * step)
                );
                entity.setLatLng(newPos);
                step++;
                requestAnimationFrame(animate);
            }
        };
        
        animate();
    }
}
```

### Offline-First Implementation

**PMTiles Configuration:**
```javascript
// Offline tile source setup
const offlineTileSource = new PMTilesSource({
    url: '/static/maps/tactical-region-{z}-{x}-{y}.pmtiles',
    maxZoom: 18
});

// Fallback to online tiles when offline data unavailable
const hybridTileLayer = L.layerGroup([
    L.tileLayer.pmtiles(offlineTileSource),
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        attribution: '© OpenStreetMap contributors',
        opacity: 0.7  // Lower opacity as fallback layer
    })
]);
```

**Service Worker for Offline Capability:**
```javascript
// Register service worker for map tile caching
if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/map-service-worker.js')
        .then(registration => {
            console.log('Map service worker registered');
        });
}
```

### Military Grid System Integration

**MGRS Overlay Implementation:**
```javascript
class MGRSOverlay {
    constructor(map) {
        this.map = map;
        this.gridLayer = L.layerGroup();
        this.coordinateDisplay = null;
    }
    
    addToMap() {
        this.updateGrid();
        this.gridLayer.addTo(this.map);
        this.setupCoordinateDisplay();
        
        // Update grid on zoom changes
        this.map.on('zoomend moveend', () => {
            this.updateGrid();
        });
    }
    
    updateGrid() {
        this.gridLayer.clearLayers();
        
        const bounds = this.map.getBounds();
        const zoom = this.map.getZoom();
        
        // Calculate appropriate grid precision for zoom level
        const precision = this.getGridPrecision(zoom);
        
        // Generate MGRS grid lines
        this.generateGridLines(bounds, precision);
    }
    
    getGridPrecision(zoom) {
        if (zoom >= 16) return 1;      // 1m grid
        if (zoom >= 14) return 10;     // 10m grid  
        if (zoom >= 12) return 100;    // 100m grid
        if (zoom >= 10) return 1000;   // 1km grid
        return 10000;                  // 10km grid
    }
    
    generateGridLines(bounds, precision) {
        // Use mgrs-js to generate grid coordinates
        const mgrsLib = new MGRS();
        
        // Convert bounds to MGRS coordinates
        const neMgrs = mgrsLib.forward([bounds.getNorth(), bounds.getEast()], precision);
        const swMgrs = mgrsLib.forward([bounds.getSouth(), bounds.getWest()], precision);
        
        // Generate grid lines between bounds
        this.drawGridLines(swMgrs, neMgrs, precision);
    }
}
```

### Recommended File Structure

```
pkg/services/web/
├── static/
│   ├── css/
│   │   └── tactical-map.css
│   ├── js/
│   │   ├── components/
│   │   │   ├── tactical-map.js
│   │   │   ├── entity-layer.js
│   │   │   ├── mgrs-overlay.js
│   │   │   └── sse-position-stream.js
│   │   ├── lib/
│   │   │   ├── leaflet/
│   │   │   ├── pmtiles/
│   │   │   └── mgrs-js/
│   │   └── map-service-worker.js
│   └── maps/
│       └── tactical-tiles/
├── templates/
│   ├── map.templ
│   ├── map_templ.go
│   └── overwatch.templ (enhanced)
└── handlers/
    ├── map.go
    └── overwatch.go (enhanced)
```

## Performance Considerations

### 60fps Real-time Updates

**Client-Side Optimization:**
- **RequestAnimationFrame**: Sync updates with browser refresh rate
- **Object Pooling**: Reuse position objects to minimize garbage collection
- **Spatial Indexing**: Efficiently query visible entities
- **Level-of-Detail**: Simplify rendering for distant entities

**Server-Side Optimization:**
- **Batched SSE Messages**: Group multiple entity updates
- **Spatial Filtering**: Only send updates for visible map area
- **Connection Pooling**: Dedicated SSE subdomain to avoid blocking

**Memory Management:**
```javascript
class EntityManager {
    constructor(maxEntities = 10000) {
        this.entities = new Map();
        this.positionPool = new Float32Array(maxEntities * 2);
        this.visibleEntities = new Set();
        this.spatialIndex = new RBush();
    }
    
    updateVisibleEntities(mapBounds) {
        // Query spatial index for entities in view
        const visibleItems = this.spatialIndex.search({
            minX: mapBounds.getWest(),
            minY: mapBounds.getSouth(),
            maxX: mapBounds.getEast(),
            maxY: mapBounds.getNorth()
        });
        
        this.visibleEntities = new Set(visibleItems.map(item => item.id));
    }
    
    shouldUpdateEntity(entityId) {
        // Only update entities currently visible
        return this.visibleEntities.has(entityId);
    }
}
```

## Integration Strategy

### Phase 1: Basic Map Integration
1. **Map Container Component**: Add `<tactical-map>` to overwatch template
2. **Static Position Display**: Show entity positions from initial KV state
3. **Basic Controls**: Pan, zoom, layer toggles

### Phase 2: Real-time Updates  
1. **SSE Integration**: Connect to existing `/api/overwatch/kv/watch`
2. **Position Streaming**: Parse entity position updates from Datastar signals
3. **Smooth Animation**: Implement interpolated position updates

### Phase 3: Advanced Features
1. **MGRS Grid System**: Military coordinate overlay
2. **Offline Capability**: PMTiles integration for disconnected operations
3. **Performance Optimization**: High-frequency update handling

### Phase 4: Tactical Enhancements
1. **Entity Tracks**: Historical position trails
2. **Mission Overlays**: Waypoints, zones, corridors
3. **Collaborative Tools**: Annotations, measurements

## Security Considerations

**Map Data Security:**
- **Tile Validation**: Verify integrity of offline map tiles
- **Access Control**: Entity visibility based on organization membership
- **Position Masking**: Configurable precision for sensitive locations

**Network Security:**
- **TLS Encryption**: All SSE connections over HTTPS
- **Session Validation**: Existing authentication integration
- **Rate Limiting**: Prevent abuse of real-time endpoints

## Future Enhancements

1. **3D Visualization**: Altitude-aware entity display
2. **Predictive Tracking**: Show projected entity paths
3. **Multi-Layer Support**: Weather, terrain, threat overlays
4. **Mobile Optimization**: Touch-optimized controls
5. **Collaborative Features**: Multi-user annotations
6. **Analytics Integration**: Heat maps, pattern analysis

## Conclusion

The proposed DTAG GIS real-time Web UI leverages Constellation Overwatch's existing strengths—robust SSE infrastructure, NATS KV global state, and high-frequency telemetry processing—while adding modern mapping capabilities for tactical situational awareness. The modular web component architecture ensures maintainability and extensibility, while offline-first design provides reliability in disconnected environments.

The integration can be implemented incrementally, starting with basic mapping and progressing to advanced tactical features, ensuring continuous value delivery while maintaining system stability.