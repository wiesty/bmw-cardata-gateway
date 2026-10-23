# BMW CarData catalogue coverage

The bridge uses a snapshot of BMW's public telematics catalogue. The snapshot contains 294 entries; 285 descriptors are included in the shared telemetry container after excluding basic data and keys for dedicated endpoints.

## Data exposed by the bridge

- Mileage, fuel level and range.
- Location, altitude and heading.
- Door, window, hood, trunk and locking status.
- Alarm and preconditioning status.
- Tyre pressures and targets.
- Battery, engine, charging, trip and climate data when provided by BMW.
- Condition-based service messages, retaining BMW's due dates and distance strings.
- Static vehicle metadata through the separate `/basicData` endpoint.

All populated source entries are exposed in `telematic_data` with BMW's original values, units and timestamps. Availability depends on the vehicle, equipment, account and latest transmission. Empty optional values are omitted. Polling more often does not imply fresher vehicle readings.

Vehicle metadata is cached. Other dedicated endpoints (image, charging history, charging settings and tyre diagnosis) are not polled by the bridge.

The repository contains no captured vehicle responses. Tests use synthetic fixtures and temporary directories.

Sources: [BMW catalogue API](https://www.bmw.co.uk/en-gb/utilities/bmw/api/cd/catalogue) and [BMW API specification](https://bmw-cardata.bmwgroup.com/customer/public/assets/swagger/swagger-customer-api-v1.json).
