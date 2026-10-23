package bmw

import (
	"sort"
	"sync"
	"time"
)

// VehicleData is the in-memory cache populated after each poll.
// Pointer fields are omitted from JSON when nil (not available for this vehicle type).
type VehicleData struct {
	BasicData                   map[string]any            `json:"basic_data,omitempty"`
	Telematics                  map[string]TelematicEntry `json:"telematic_data,omitempty"`
	Latitude                    *float64                  `json:"latitude,omitempty"`
	Longitude                   *float64                  `json:"longitude,omitempty"`
	AltitudeM                   *float64                  `json:"altitude_m,omitempty"`
	HeadingDeg                  *float64                  `json:"heading_deg,omitempty"`
	WindowFLStatus              *string                   `json:"window_front_left_status,omitempty"`
	WindowFRStatus              *string                   `json:"window_front_right_status,omitempty"`
	WindowRLStatus              *string                   `json:"window_rear_left_status,omitempty"`
	WindowRRStatus              *string                   `json:"window_rear_right_status,omitempty"`
	AlarmArmedStatus            *string                   `json:"alarm_armed_status,omitempty"`
	AlarmActive                 *bool                     `json:"alarm_active,omitempty"`
	PreconditioningStatus       *string                   `json:"preconditioning_status,omitempty"`
	PreconditioningRemainingMin *float64                  `json:"preconditioning_remaining_min,omitempty"`
	BatteryVoltageV             *float64                  `json:"battery_voltage_v,omitempty"`
	CoolantTemperature          *float64                  `json:"coolant_temperature,omitempty"`
	TireTargetFLkPa             *float64                  `json:"tire_pressure_target_fl_kpa,omitempty"`
	TireTargetFRkPa             *float64                  `json:"tire_pressure_target_fr_kpa,omitempty"`
	TireTargetRLkPa             *float64                  `json:"tire_pressure_target_rl_kpa,omitempty"`
	TireTargetRRkPa             *float64                  `json:"tire_pressure_target_rr_kpa,omitempty"`
	ServiceMessages             []ServiceMessage          `json:"service_messages,omitempty"`
	MileageKm                   float64                   `json:"mileage_km"`
	LastUpdate                  time.Time                 `json:"last_update"`

	RangeKm     *float64 `json:"range_km,omitempty"`
	RangeElecKm *float64 `json:"range_electric_km,omitempty"`
	RangeFuelKm *float64 `json:"range_fuel_km,omitempty"`

	BatterySocPct            *float64 `json:"battery_soc_pct,omitempty"`
	BatterySocTargetPct      *float64 `json:"battery_soc_target_pct,omitempty"`
	ChargingStatus           *string  `json:"charging_status,omitempty"`
	ChargingPowerKw          *float64 `json:"charging_power_kw,omitempty"`
	ChargingTimeRemainingMin *float64 `json:"charging_time_remaining_min,omitempty"`
	IsPluggedIn              *bool    `json:"is_plugged_in,omitempty"`

	FuelLevelPct    *float64 `json:"fuel_level_pct,omitempty"`
	FuelLevelLiters *float64 `json:"fuel_level_liters,omitempty"`

	IsMoving     *bool `json:"is_moving,omitempty"`
	IsIgnitionOn *bool `json:"is_ignition_on,omitempty"`
	IsEngineOn   *bool `json:"is_engine_on,omitempty"`

	DoorsLocked *string `json:"doors_locked,omitempty"`
	DoorsStatus *string `json:"doors_status,omitempty"`
	DoorFLOpen  *bool   `json:"door_front_left_open,omitempty"`
	DoorFROpen  *bool   `json:"door_front_right_open,omitempty"`
	DoorRLOpen  *bool   `json:"door_rear_left_open,omitempty"`
	DoorRROpen  *bool   `json:"door_rear_right_open,omitempty"`
	TrunkOpen   *bool   `json:"trunk_open,omitempty"`
	HoodOpen    *bool   `json:"hood_open,omitempty"`
	LightsOn    *bool   `json:"lights_on,omitempty"`

	TirePressureFLkPa *float64 `json:"tire_pressure_fl_kpa,omitempty"`
	TirePressureFRkPa *float64 `json:"tire_pressure_fr_kpa,omitempty"`
	TirePressureRLkPa *float64 `json:"tire_pressure_rl_kpa,omitempty"`
	TirePressureRRkPa *float64 `json:"tire_pressure_rr_kpa,omitempty"`
	TireDiagnosis     *string  `json:"tire_diagnosis,omitempty"`

	ServiceDistanceKm    *float64 `json:"service_distance_km,omitempty"`
	CheckControlMessages *string  `json:"check_control_messages,omitempty"`
}

// ServiceMessage preserves BMW's CBS fields without guessing dates or distance units.
type ServiceMessage struct {
	ID                int    `json:"id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	Date              string `json:"date,omitempty"`
	Description       string `json:"description,omitempty"`
	Text              string `json:"text,omitempty"`
	MessageType       string `json:"messageType,omitempty"`
	DistanceRemaining string `json:"unitOfLengthRemaining,omitempty"`
}

// Cache is the thread-safe in-memory store for the latest VehicleData.
type Cache struct {
	mu      sync.RWMutex
	data    *VehicleData
	polling PollStatus
}

func NewCache() *Cache { return &Cache{} }

func (c *Cache) Set(d VehicleData) {
	c.mu.Lock()
	c.data = &d
	c.mu.Unlock()
}

func (c *Cache) Get() *VehicleData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// VehicleRegistry is a thread-safe map from VIN to Cache for multi-vehicle support.
type VehicleRegistry struct {
	mu     sync.RWMutex
	caches map[string]*Cache
}

func NewRegistry() *VehicleRegistry {
	return &VehicleRegistry{caches: make(map[string]*Cache)}
}

func (r *VehicleRegistry) Add(vin string, c *Cache) {
	r.mu.Lock()
	r.caches[vin] = c
	r.mu.Unlock()
}

func (r *VehicleRegistry) Get(vin string) *Cache {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.caches[vin]
}

// VINs returns all registered VINs in sorted order.
func (r *VehicleRegistry) VINs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vins := make([]string, 0, len(r.caches))
	for v := range r.caches {
		vins = append(vins, v)
	}
	sort.Strings(vins)
	return vins
}
