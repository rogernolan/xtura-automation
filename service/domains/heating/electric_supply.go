package heating

// ElectricSupplyState describes selected heating demand and the configured
// shore allowance, not measured heater consumption. Unknown snapshots must
// never be interpreted as a disconnected shore supply.
type ElectricSupplyState struct {
	Known          bool
	HeaterOn       bool
	ElectricWatts  float64
	ShoreConnected bool
	InputLimitA    float64
	InputVoltageV  float64
	ShoreWatts     float64
	Session        uint64
}
