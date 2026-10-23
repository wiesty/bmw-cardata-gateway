package api

const openapiSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Wiestys Unofficial BMW CarData Gateway",
    "description": "Minimal REST bridge for BMW CarData. Simplified wrapper around the official BMW CarData API — exposes vehicle telemetry as a local REST endpoint.\n\n**Rate limit:** 50 REST requests per day, reset at 00:00 UTC. Vehicle discovery and container management also consume requests. Default poll interval: 60 min per vehicle; increase it for multiple vehicles.\n\n**Authentication:** If API_KEY env var is set, all endpoints (except /docs) require X-API-Key or Authorization: Bearer header.\n\n**Note:** Fields that don't apply to your vehicle type (e.g. battery fields on ICE, fuel fields on BEV) are omitted from the response.",
    "version": "1.2.1"
  },
  "servers": [
    { "url": "http://localhost:8080", "description": "Local" }
  ],
  "components": {
    "securitySchemes": {
      "ApiKeyHeader": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-Key",
        "description": "Required only when API_KEY env var is set. Can also be passed as Authorization: Bearer <key>."
      }
    },
    "schemas": {
      "Health": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "enum": ["ok", "starting", "degraded"] },
          "last_update": { "type": "string", "format": "date-time", "nullable": true },
          "polling": { "type": "object", "additionalProperties": { "$ref": "#/components/schemas/PollStatus" } }
        }
      },
      "PollStatus": {
        "type": "object",
        "properties": {
          "state": { "type": "string", "enum": ["waiting", "polling", "degraded"] },
          "last_attempt": { "type": "string", "format": "date-time" },
          "last_success": { "type": "string", "format": "date-time" },
          "next_poll": { "type": "string", "format": "date-time" },
          "last_error": { "type": "string" },
          "consecutive_failures": { "type": "integer" }
        }
      },
      "VehicleEntry": {
        "type": "object",
        "required": ["vin", "ready"],
        "properties": {
          "vin":         { "type": "string", "description": "Vehicle Identification Number" },
          "last_update": { "type": "string", "format": "date-time", "nullable": true, "description": "Timestamp of last successful poll" },
          "ready":       { "type": "boolean", "description": "Whether data is available" },
          "polling": { "$ref": "#/components/schemas/PollStatus" }
        }
      },
      "Vehicle": {
        "type": "object",
        "required": ["mileage_km", "last_update"],
        "properties": {
          "basic_data": {
            "type": "object",
            "additionalProperties": true,
            "description": "Static BMW vehicle metadata, including model, engine, production date and equipment. Fetched once and persisted."
          },
          "telematic_data": {
            "type": "object",
            "description": "All populated BMW descriptors with original string values, units and source timestamps. Empty descriptors are omitted.",
            "additionalProperties": {
              "type": "object",
              "properties": {
                "value": {
                  "type": "string"
                },
                "unit": {
                  "type": "string",
                  "nullable": true
                },
                "timestamp": {
                  "type": "string",
                  "format": "date-time",
                  "nullable": true
                }
              }
            }
          },
          "service_messages": {
            "type": "array",
            "description": "Parsed BMW condition-based service messages. Original BMW field names and distance strings are preserved.",
            "items": {
              "type": "object",
              "properties": {
                "id": {
                  "type": "integer"
                },
                "title": {
                  "type": "string"
                },
                "status": {
                  "type": "string"
                },
                "date": {
                  "type": "string"
                },
                "description": {
                  "type": "string"
                },
                "text": {
                  "type": "string"
                },
                "messageType": {
                  "type": "string"
                },
                "unitOfLengthRemaining": {
                  "type": "string"
                }
              }
            }
          },
          "latitude": {
            "type": "number"
          },
          "longitude": {
            "type": "number"
          },
          "altitude_m": {
            "type": "number"
          },
          "heading_deg": {
            "type": "number"
          },
          "preconditioning_remaining_min": {
            "type": "number"
          },
          "battery_voltage_v": {
            "type": "number"
          },
          "coolant_temperature": {
            "type": "number",
            "description": "Original BMW temperature value; see telematic_data for its unit."
          },
          "tire_pressure_target_fl_kpa": {
            "type": "number"
          },
          "tire_pressure_target_fr_kpa": {
            "type": "number"
          },
          "tire_pressure_target_rl_kpa": {
            "type": "number"
          },
          "tire_pressure_target_rr_kpa": {
            "type": "number"
          },
          "window_front_left_status": {
            "type": "string"
          },
          "window_front_right_status": {
            "type": "string"
          },
          "window_rear_left_status": {
            "type": "string"
          },
          "window_rear_right_status": {
            "type": "string"
          },
          "alarm_armed_status": {
            "type": "string"
          },
          "preconditioning_status": {
            "type": "string"
          },
          "alarm_active": {
            "type": "boolean"
          },
          "mileage_km":   { "type": "number", "description": "Odometer reading (km)" },
          "last_update":  { "type": "string", "format": "date-time", "description": "Timestamp of last successful poll" },

          "range_km":          { "type": "number", "description": "Best available total range (km)" },
          "range_electric_km": { "type": "number", "description": "Electric range (BEV/PHEV, km)" },
          "range_fuel_km":     { "type": "number", "description": "Combustion range (ICE/PHEV, km)" },

          "battery_soc_pct":            { "type": "number", "description": "Battery state of charge (%)" },
          "battery_soc_target_pct":     { "type": "number", "description": "Charging target SoC (%)" },
          "charging_status":            { "type": "string", "description": "Charging status (e.g. CHARGING, NOT_CHARGING)" },
          "charging_power_kw":          { "type": "number", "description": "Current charging power (kW)" },
          "charging_time_remaining_min":{ "type": "number", "description": "Minutes until fully charged" },
          "is_plugged_in":              { "type": "boolean", "description": "Charging cable connected" },

          "fuel_level_pct":    { "type": "number", "description": "Fuel tank level (%)" },
          "fuel_level_liters": { "type": "number", "description": "Remaining fuel (liters)" },

          "is_moving":      { "type": "boolean", "description": "Vehicle is currently moving" },
          "is_ignition_on": { "type": "boolean", "description": "Ignition is on" },
          "is_engine_on":   { "type": "boolean", "description": "Engine is running" },

          "doors_locked":         { "type": "string",  "description": "Overall door lock state (e.g. LOCKED)" },
          "doors_status":         { "type": "string",  "description": "Overall door open/closed state" },
          "door_front_left_open": { "type": "boolean", "description": "Front left door open" },
          "door_front_right_open":{ "type": "boolean", "description": "Front right door open" },
          "door_rear_left_open":  { "type": "boolean", "description": "Rear left door open" },
          "door_rear_right_open": { "type": "boolean", "description": "Rear right door open" },
          "trunk_open":           { "type": "boolean", "description": "Trunk open" },
          "hood_open":            { "type": "boolean", "description": "Hood/bonnet open" },
          "lights_on":            { "type": "boolean", "description": "Running lights active" },

          "tire_pressure_fl_kpa": { "type": "number", "description": "Front left tire pressure (kPa)" },
          "tire_pressure_fr_kpa": { "type": "number", "description": "Front right tire pressure (kPa)" },
          "tire_pressure_rl_kpa": { "type": "number", "description": "Rear left tire pressure (kPa)" },
          "tire_pressure_rr_kpa": { "type": "number", "description": "Rear right tire pressure (kPa)" },
          "tire_diagnosis":       { "type": "string",  "description": "Tire system diagnosis" },

          "service_distance_km":    { "type": "number", "description": "Remaining distance until next service (km)" },
          "check_control_messages": { "type": "string",  "description": "Active check control messages" }
        }
      },
      "Error": {
        "type": "object",
        "properties": {
          "error": { "type": "string" }
        }
      }
    }
  },
  "security": [],
  "paths": {
    "/health": {
      "get": {
        "summary": "Health check",
        "description": "Returns service status and timestamp of last successful poll across all tracked vehicles.",
        "responses": {
          "200": {
            "description": "Service running with data; status may be degraded when polling failed",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Health" },
                "example": { "status": "ok", "last_update": "2026-04-19T12:00:00Z" }
              }
            }
          },
          "503": {
            "description": "Waiting for first poll (startup)",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Health" },
                "example": { "status": "starting", "last_update": null }
              }
            }
          }
        }
      }
    },
    "/vehicles": {
      "get": {
        "summary": "List tracked vehicles",
        "description": "Returns all VINs currently tracked by this bridge instance with their data availability status.",
        "responses": {
          "200": {
            "description": "List of tracked vehicles",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": { "$ref": "#/components/schemas/VehicleEntry" }
                },
                "example": [
                  { "vin": "WBA12345678901234", "last_update": "2026-04-19T12:00:00Z", "ready": true },
                  { "vin": "WBA98765432109876", "last_update": null, "ready": false }
                ]
              }
            }
          }
        }
      }
    },
    "/vehicle/{vin}": {
      "get": {
        "summary": "Vehicle telemetry by VIN",
        "description": "Returns the latest cached data for the specified VIN. Use this endpoint when tracking multiple vehicles.",
        "parameters": [
          {
            "name": "vin",
            "in": "path",
            "required": true,
            "schema": { "type": "string" },
            "description": "Vehicle Identification Number (case-insensitive)"
          }
        ],
        "responses": {
          "200": {
            "description": "Latest vehicle data",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Vehicle" }
              }
            }
          },
          "404": {
            "description": "VIN not tracked by this bridge instance",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Error" } } }
          },
          "503": {
            "description": "No data yet (first poll pending)",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Error" } } }
          }
        }
      }
    },
    "/vehicle": {
      "get": {
        "summary": "Vehicle telemetry (single-vehicle shortcut)",
        "description": "Returns the latest cached vehicle data. Works only when a single VIN is tracked. If multiple VINs are configured, use /vehicle/{vin} instead.",
        "responses": {
          "200": {
            "description": "Latest vehicle data",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Vehicle" },
                "examples": {
                  "bev": {
                    "summary": "BEV (full electric)",
                    "value": {
                      "mileage_km": 12345, "last_update": "2026-04-19T12:00:00Z",
                      "range_km": 380, "range_electric_km": 380,
                      "battery_soc_pct": 92.5, "battery_soc_target_pct": 100,
                      "charging_status": "CHARGING", "charging_power_kw": 11.0,
                      "charging_time_remaining_min": 45, "is_plugged_in": true,
                      "is_moving": false, "is_ignition_on": false, "is_engine_on": false,
                      "doors_locked": "LOCKED", "doors_status": "CLOSED",
                      "door_front_left_open": false, "door_front_right_open": false,
                      "door_rear_left_open": false, "door_rear_right_open": false,
                      "trunk_open": false, "hood_open": false, "lights_on": false,
                      "tire_pressure_fl_kpa": 250, "tire_pressure_fr_kpa": 250,
                      "tire_pressure_rl_kpa": 240, "tire_pressure_rr_kpa": 240,
                      "tire_diagnosis": "OK", "service_distance_km": 8500
                    }
                  },
                  "ice": {
                    "summary": "ICE (combustion engine)",
                    "value": {
                      "mileage_km": 45000, "last_update": "2026-04-19T12:00:00Z",
                      "range_km": 520, "range_fuel_km": 520,
                      "fuel_level_pct": 78.0, "fuel_level_liters": 46.8,
                      "is_moving": false, "is_ignition_on": false, "is_engine_on": false,
                      "doors_locked": "LOCKED", "doors_status": "CLOSED",
                      "trunk_open": false, "service_distance_km": 12000
                    }
                  }
                }
              }
            }
          },
          "400": {
            "description": "Multiple vehicles tracked — use /vehicle/{vin}",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Error" } } }
          },
          "503": {
            "description": "No data yet (first poll pending)",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Error" } } }
          }
        }
      }
    }
  }
}`

const swaggerUI = `<!DOCTYPE html>
<html>
<head>
  <title>Unofficial BMW CarData Gateway — API Docs</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  SwaggerUIBundle({
    url: "/openapi.json",
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
    layout: "BaseLayout"
  })
</script>
</body>
</html>`
