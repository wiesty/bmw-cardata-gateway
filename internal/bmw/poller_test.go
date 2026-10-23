package bmw

import "testing"

func TestBMWOpenClosedSensors(t *testing.T) {
	for _, key := range []string{"vehicle.cabin.door.row1.driver.isOpen", "vehicle.cabin.door.row1.passenger.isOpen", "vehicle.body.hood.isOpen", "vehicle.body.trunk.isOpen"} {
		for _, tc := range []struct {
			value string
			want  bool
		}{{"OPEN", true}, {"CLOSED", false}, {"true", true}, {"false", false}} {
			t.Run(key+"/"+tc.value, func(t *testing.T) {
				value := tc.value
				got := getBoolPtr(map[string]TelematicEntry{key: {Value: &value}}, key)
				if got == nil || *got != tc.want {
					t.Fatalf("got %v, want %t", got, tc.want)
				}
			})
		}
	}
}

func TestUnknownSensorValuesRemainUnavailable(t *testing.T) {
	for _, value := range []string{"", "UNKNOWN", "NOT_AVAILABLE"} {
		key := "vehicle.body.hood.isOpen"
		if got := getBoolPtr(map[string]TelematicEntry{key: {Value: &value}}, key); got != nil {
			t.Fatalf("%q interpreted as %t", value, *got)
		}
	}
	if got := getBoolPtr(nil, "vehicle.body.hood.isOpen"); got != nil {
		t.Fatal("missing sensor interpreted as available")
	}
}
