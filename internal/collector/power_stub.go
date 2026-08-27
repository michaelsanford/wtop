//go:build !windows

package collector

// collectPowerNative reports unknown rather than absent on non-Windows builds.
// "No battery" is a positive claim about the hardware; a platform with no
// implementation has not made it.
func collectPowerNative() PowerSnapshot {
	return PowerSnapshot{AC: ACUnknown, State: BatteryUnknown}
}
