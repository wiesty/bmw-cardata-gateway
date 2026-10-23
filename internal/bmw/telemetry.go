package bmw

import (
	"encoding/json"
	"time"
)

func mapVehicleData(data map[string]TelematicEntry, basicData map[string]any, now time.Time) VehicleData {
	d := VehicleData{
		MileageKm:  getFloat(data, "vehicle.vehicle.travelledDistance"),
		LastUpdate: now.UTC(),
	}

	elecRange := getFloatPtr(data, "vehicle.drivetrain.electricEngine.kombiRemainingElectricRange")
	totalRange := getFloatPtr(data, "vehicle.drivetrain.totalRemainingRange")
	fuelRange := getFloatPtr(data, "vehicle.drivetrain.lastRemainingRange")
	d.RangeElecKm = elecRange
	d.RangeFuelKm = fuelRange
	switch {
	case totalRange != nil && *totalRange > 0:
		d.RangeKm = totalRange
	case elecRange != nil && *elecRange > 0:
		d.RangeKm = elecRange
	case fuelRange != nil && *fuelRange > 0:
		d.RangeKm = fuelRange
	}

	d.BatterySocPct = getFloatPtr(data, "vehicle.drivetrain.electricEngine.charging.level")
	d.BatterySocTargetPct = getFloatPtr(data, "vehicle.powertrain.electric.battery.stateOfCharge.target")
	d.ChargingStatus = getStringPtr(data, "vehicle.drivetrain.electricEngine.charging.status")
	d.ChargingPowerKw = getFloatPtr(data, "vehicle.powertrain.electric.battery.charging.power")
	d.ChargingTimeRemainingMin = getFloatPtr(data, "vehicle.drivetrain.electricEngine.charging.timeToFullyCharged")
	d.IsPluggedIn = getBoolPtr(data, "vehicle.powertrain.tractionBattery.charging.port.anyPosition.isPlugged")

	d.FuelLevelPct = getFloatPtr(data, "vehicle.drivetrain.fuelSystem.level")
	d.FuelLevelLiters = getFloatPtr(data, "vehicle.drivetrain.fuelSystem.remainingFuel")

	d.IsMoving = getBoolPtr(data, "vehicle.isMoving")
	d.IsIgnitionOn = getBoolPtr(data, "vehicle.drivetrain.engine.isIgnitionOn")
	d.IsEngineOn = getBoolPtr(data, "vehicle.drivetrain.engine.isActive")

	d.DoorsLocked = getStringPtr(data, "vehicle.cabin.door.lock.status")
	d.DoorsStatus = getStringPtr(data, "vehicle.cabin.door.status")
	d.DoorFLOpen = getBoolPtr(data, "vehicle.cabin.door.row1.driver.isOpen")
	d.DoorFROpen = getBoolPtr(data, "vehicle.cabin.door.row1.passenger.isOpen")
	d.DoorRLOpen = getBoolPtr(data, "vehicle.cabin.door.row2.driver.isOpen")
	d.DoorRROpen = getBoolPtr(data, "vehicle.cabin.door.row2.passenger.isOpen")
	d.TrunkOpen = getBoolPtr(data, "vehicle.body.trunk.isOpen")
	d.HoodOpen = getBoolPtr(data, "vehicle.body.hood.isOpen")
	d.LightsOn = getBoolPtr(data, "vehicle.body.lights.isRunningOn")

	d.TirePressureFLkPa = getFloatPtr(data, "vehicle.chassis.axle.row1.wheel.left.tire.pressure")
	d.TirePressureFRkPa = getFloatPtr(data, "vehicle.chassis.axle.row1.wheel.right.tire.pressure")
	d.TirePressureRLkPa = getFloatPtr(data, "vehicle.chassis.axle.row2.wheel.left.tire.pressure")
	d.TirePressureRRkPa = getFloatPtr(data, "vehicle.chassis.axle.row2.wheel.right.tire.pressure")
	d.TireDiagnosis = getStringPtr(data, "vehicle.chassis.axle.wheel.tire.diagnosis")

	d.ServiceDistanceKm = getFloatPtr(data, "vehicle.status.serviceDistance.next")
	d.CheckControlMessages = getStringPtr(data, "vehicle.status.checkControlMessages")

	d.BasicData = basicData
	d.Telematics = make(map[string]TelematicEntry)
	for key, entry := range data {
		if entry.Value != nil && *entry.Value != "" {
			d.Telematics[key] = entry
		}
	}
	d.Latitude = getFloatPtr(data, "vehicle.cabin.infotainment.navigation.currentLocation.latitude")
	d.Longitude = getFloatPtr(data, "vehicle.cabin.infotainment.navigation.currentLocation.longitude")
	d.AltitudeM = getFloatPtr(data, "vehicle.cabin.infotainment.navigation.currentLocation.altitude")
	d.HeadingDeg = getFloatPtr(data, "vehicle.cabin.infotainment.navigation.currentLocation.heading")
	d.WindowFLStatus = getStringPtr(data, "vehicle.cabin.window.row1.driver.status")
	d.WindowFRStatus = getStringPtr(data, "vehicle.cabin.window.row1.passenger.status")
	d.WindowRLStatus = getStringPtr(data, "vehicle.cabin.window.row2.driver.status")
	d.WindowRRStatus = getStringPtr(data, "vehicle.cabin.window.row2.passenger.status")
	d.AlarmArmedStatus = getStringPtr(data, "vehicle.vehicle.antiTheftAlarmSystem.alarm.armStatus")
	d.AlarmActive = getBoolPtr(data, "vehicle.vehicle.antiTheftAlarmSystem.alarm.isOn")
	d.PreconditioningStatus = getStringPtr(data, "vehicle.vehicle.preConditioning.activity")
	d.PreconditioningRemainingMin = getFloatPtr(data, "vehicle.vehicle.preConditioning.remainingTime")
	d.BatteryVoltageV = getFloatPtr(data, "vehicle.electricalSystem.battery.voltage")
	d.CoolantTemperature = getFloatPtr(data, "vehicle.drivetrain.internalCombustionEngine.engine.ect")
	d.TireTargetFLkPa = getFloatPtr(data, "vehicle.chassis.axle.row1.wheel.left.tire.pressureTarget")
	d.TireTargetFRkPa = getFloatPtr(data, "vehicle.chassis.axle.row1.wheel.right.tire.pressureTarget")
	d.TireTargetRLkPa = getFloatPtr(data, "vehicle.chassis.axle.row2.wheel.left.tire.pressureTarget")
	d.TireTargetRRkPa = getFloatPtr(data, "vehicle.chassis.axle.row2.wheel.right.tire.pressureTarget")
	if value := getStringPtr(data, "vehicle.status.conditionBasedServices"); value != nil {
		var messages []ServiceMessage
		if json.Unmarshal([]byte(*value), &messages) == nil {
			for i := range messages {
				if messages[i].Date == "null" {
					messages[i].Date = ""
				}
			}
			d.ServiceMessages = messages
		}
	}
	// BMW reports charging power in W; the bridge exposes kW.
	if entry := data["vehicle.powertrain.electric.battery.charging.power"]; d.ChargingPowerKw != nil && entry.Unit != nil && *entry.Unit == "W" {
		*d.ChargingPowerKw /= 1000
	}
	return d
}
