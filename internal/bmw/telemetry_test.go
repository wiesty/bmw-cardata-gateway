package bmw

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestExpandedTelemetryPreservesSourceAndServiceMessages(t *testing.T) {
	data := make(map[string]TelematicEntry)
	add := func(key, value, unit string) {
		timestamp := "2000-01-01T00:00:00Z"
		data[key] = TelematicEntry{Value: &value, Unit: &unit, Timestamp: &timestamp}
	}
	add("vehicle.cabin.infotainment.navigation.currentLocation.latitude", "0.25", "degrees")
	add("vehicle.cabin.window.row1.driver.status", "CLOSED", "")
	add("vehicle.vehicle.antiTheftAlarmSystem.alarm.isOn", "false", "")
	add("vehicle.chassis.axle.row1.wheel.left.tire.pressureTarget", "250", "kPa")
	add("vehicle.powertrain.electric.battery.charging.power", "11000", "W")
	add("vehicle.status.conditionBasedServices", `[{"id":999,"title":"Synthetic service","status":"PENDING","date":"null","unitOfLengthRemaining":"12345"}]`, "")
	data["missing"] = TelematicEntry{}
	got := mapVehicleData(data, map[string]any{"modelName": "Test coupe", "hasSunRoof": false}, time.Now())
	if got.Latitude == nil || *got.Latitude != 0.25 || got.WindowFLStatus == nil || *got.WindowFLStatus != "CLOSED" || got.AlarmActive == nil || *got.AlarmActive {
		t.Fatalf("expanded fields not mapped: %+v", got)
	}
	if got.ChargingPowerKw == nil || *got.ChargingPowerKw != 11 {
		t.Fatal("charging power was not converted to kW")
	}
	if len(got.Telematics) != 6 || *got.Telematics["vehicle.powertrain.electric.battery.charging.power"].Value != "11000" {
		t.Fatal("source values were lost or empty values retained")
	}
	if len(got.ServiceMessages) != 1 || got.ServiceMessages[0].Date != "" || got.ServiceMessages[0].DistanceRemaining != "12345" {
		t.Fatalf("incorrect CBS parsing: %+v", got.ServiceMessages)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["alarm_active"] != false || decoded["basic_data"].(map[string]any)["hasSunRoof"] != false {
		t.Fatal("false values were omitted")
	}
}

func TestMalformedServiceMessagesKeepRawData(t *testing.T) {
	value := "not-json"
	got := mapVehicleData(map[string]TelematicEntry{"vehicle.status.conditionBasedServices": {Value: &value}}, nil, time.Now())
	if got.ServiceMessages != nil || len(got.Telematics) != 1 {
		t.Fatal("malformed service messages were not preserved as raw data")
	}
}

func TestASNBooleanValues(t *testing.T) {
	key := "vehicle.isMoving"
	for _, tc := range []struct {
		value string
		want  *bool
	}{{"ASN_isTrue", boolPtr(true)}, {"ASN_isFalse", boolPtr(false)}, {"ASN_isUnknown", nil}} {
		got := getBoolPtr(map[string]TelematicEntry{key: {Value: &tc.value}}, key)
		if tc.want == nil {
			if got != nil {
				t.Fatal("unknown became a boolean")
			}
			continue
		}
		if got == nil || *got != *tc.want {
			t.Fatalf("incorrect ASN boolean %q", tc.value)
		}
	}
}

func boolPtr(value bool) *bool { return &value }

func TestDescriptorChangesInvalidateContainerAtSameCount(t *testing.T) {
	if descriptorContainerName([]string{"a", "b"}) == descriptorContainerName([]string{"a", "c"}) {
		t.Fatal("changed descriptor reused the old container")
	}
}

func TestRestartReusesBasicDataWithoutBMWRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, &state{VIN: "TEST", ContainerID: "container", LastPollTime: time.Now(), CachedData: &VehicleData{BasicData: map[string]any{"modelName": "Test"}}}); err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(*http.Request) (int, string) {
		t.Fatal("restart requested BMW data before the interval elapsed")
		return 500, ""
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache := NewCache()
	StartPoller(ctx, client, "TEST", "container", time.Hour, cache, path)
	if cache.Get() == nil || cache.Get().BasicData["modelName"] != "Test" {
		t.Fatal("metadata cache not restored")
	}
}

func TestBasicDataRetainsBMWTypes(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/customers/vehicles/TEST/basicData" {
			t.Fatalf("unexpected URL %s", r.URL)
		}
		return 200, `{"modelName":"Test","hasSunRoof":false,"numberOfDoors":2}`
	})
	got, err := client.getBasicData(context.Background(), "TEST")
	if err != nil || got["hasSunRoof"] != false || got["numberOfDoors"] != float64(2) {
		t.Fatalf("basic data = %v, %v", got, err)
	}
}
