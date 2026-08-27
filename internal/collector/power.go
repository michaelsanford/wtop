package collector

import "time"

// ACLineState is the mains power state reported by the OS.
type ACLineState int

const (
	ACUnknown ACLineState = iota
	ACOffline
	ACOnline
)

// BatteryState summarises what the battery is doing.  Absent and Unknown are
// deliberately distinct: a desktop with no battery is a fact worth rendering
// differently from a machine that declined to answer.
type BatteryState int

const (
	BatteryUnknown BatteryState = iota
	BatteryAbsent
	BatteryCharging
	BatteryDischarging
	BatteryFull
)

// PowerSnapshot holds AC and battery status.
//
// The *Valid flags exist because Windows reports "unknown" in-band, with
// sentinel values that are indistinguishable from plausible readings if taken at
// face value: an unknown charge is 255, which would render as 255%.
type PowerSnapshot struct {
	AC      ACLineState
	Present bool // a battery is physically installed
	State   BatteryState

	Pct      float64
	PctValid bool

	Remaining      time.Duration
	RemainingValid bool

	Saver bool // battery-saver / power-saving mode is engaged

	// From CallNtPowerInformation, best-effort and often unavailable.
	RateMilliW int32 // positive charging, negative discharging
	RateValid  bool
}

// SYSTEM_POWER_STATUS field values.
const (
	acLineOffline = 0
	acLineOnline  = 1
	acLineUnknown = 255

	batteryFlagHigh     = 0x01
	batteryFlagLow      = 0x02
	batteryFlagCritical = 0x04
	batteryFlagCharging = 0x08
	batteryFlagNoBatt   = 0x80
	batteryFlagUnknown  = 0xFF

	batteryPctUnknown  = 255
	batteryTimeUnknown = 0xFFFFFFFF

	// SYSTEM_STATUS_FLAG: battery saver is on.
	powerSaverOn = 1
)

// systemPowerStatus mirrors the Win32 SYSTEM_POWER_STATUS struct.  It lives in
// the portable file, and holds no pointers, so decodeSystemPowerStatus can be
// exercised on any platform.
type systemPowerStatus struct {
	ACLineStatus        uint8
	BatteryFlag         uint8
	BatteryLifePercent  uint8
	SystemStatusFlag    uint8
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// decodeSystemPowerStatus turns the raw Win32 struct into a PowerSnapshot,
// mapping every "unknown" sentinel onto an explicit invalid flag rather than
// letting it through as data.
func decodeSystemPowerStatus(s systemPowerStatus) PowerSnapshot {
	var p PowerSnapshot

	switch s.ACLineStatus {
	case acLineOffline:
		p.AC = ACOffline
	case acLineOnline:
		p.AC = ACOnline
	default: // acLineUnknown and anything undocumented
		p.AC = ACUnknown
	}

	p.Saver = s.SystemStatusFlag == powerSaverOn

	switch {
	case s.BatteryFlag == batteryFlagUnknown:
		// The machine may or may not have a battery; it did not say.
		p.Present, p.State = false, BatteryUnknown
		return p
	case s.BatteryFlag&batteryFlagNoBatt != 0:
		p.Present, p.State = false, BatteryAbsent
		return p
	}

	p.Present = true

	if s.BatteryLifePercent != batteryPctUnknown {
		p.Pct, p.PctValid = float64(s.BatteryLifePercent), true
	}

	switch {
	case s.BatteryFlag&batteryFlagCharging != 0:
		p.State = BatteryCharging
	case p.PctValid && p.Pct >= 100 && p.AC == ACOnline:
		p.State = BatteryFull
	case s.BatteryFlag&(batteryFlagHigh|batteryFlagLow|batteryFlagCritical) != 0:
		p.State = BatteryDischarging
	case p.AC == ACOnline:
		p.State = BatteryFull
	default:
		p.State = BatteryDischarging
	}

	// The remaining-time estimate is only meaningful while running on battery;
	// Windows reports it as unknown on AC anyway.
	if s.BatteryLifeTime != batteryTimeUnknown {
		p.Remaining = time.Duration(s.BatteryLifeTime) * time.Second
		p.RemainingValid = true
	}

	return p
}

// collectPower returns the current AC/battery state.  It has no error return:
// an unavailable sensor is expressed as Unknown, which the UI must handle
// regardless, so there is nothing a caller could do with an error.
func collectPower() PowerSnapshot {
	return collectPowerNative()
}
