# HEARTBEAT messages
nats --server nats://gondola.proxy.rlwy.net:33026 pub constellation.telemetry.test-org.drone-{{Count}} --count 10 '{"message_type":"HEARTBEAT","entity_id":"drone-{{Count}}","data":{"custom_mode":3,"base_mode":217,"system_status":4,"autopilot":3,"type":2},"timestamp":"{{TimeRFC3339}}"}'

# GPS position updates
nats --server nats://gondola.proxy.rlwy.net:33026 pub constellation.telemetry.test-org.drone-001 --count 10 '{"message_type":"GPS_RAW_INT","entity_id":"drone-001","data":{"lat":377490000,"lon":-1224190000,"alt":{{Random 45000 55000}},"fix_type":3,"satellites_visible":12},"timestamp":"{{TimeRFC3339}}"}'

# Battery status
nats --server nats://gondola.proxy.rlwy.net:33026 pub constellation.telemetry.test-org.drone-001 --count 10 '{"message_type":"BATTERY_STATUS","entity_id":"drone-001","data":{"battery_remaining":{{Random 70 95}},"current_battery":1500,"voltage_battery":22200},"timestamp":"{{TimeRFC3339}}"}'