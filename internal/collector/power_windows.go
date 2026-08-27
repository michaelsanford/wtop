//go:build windows

package collector

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetSystemPowerStatus = modKernel32.NewProc("GetSystemPowerStatus")

	modPowrprof                = windows.NewLazySystemDLL("powrprof.dll")
	procCallNtPowerInformation = modPowrprof.NewProc("CallNtPowerInformation")
)

// CallNtPowerInformation InformationLevel values.
const systemBatteryState = 5

// rateUnknown is what SYSTEM_BATTERY_STATE.Rate carries when the driver cannot
// report a charge/discharge rate.
const rateUnknown = math.MinInt32

// systemBatteryStateStruct mirrors SYSTEM_BATTERY_STATE.  The four leading
// fields are BOOLEAN (one byte each), so the struct needs no explicit padding
// before the uint32 run.
type systemBatteryStateStruct struct {
	AcOnLine          byte
	BatteryPresent    byte
	Charging          byte
	Discharging       byte
	_                 [3]byte
	_                 byte
	MaxCapacity       uint32
	RemainingCapacity uint32
	Rate              int32
	EstimatedTime     uint32
	DefaultAlert1     uint32
	DefaultAlert2     uint32
}

// collectPowerNative reads AC/battery state.
//
// GetSystemPowerStatus is a plain kernel call against state the power manager
// already caches — no WMI, no COM, no RPC — so it is cheap enough to sit on the
// per-tick critical path.  Win32_Battery would cost 50-200 ms and would have to
// move to the cached path instead.
func collectPowerNative() PowerSnapshot {
	var raw systemPowerStatus
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&raw)))
	if r == 0 {
		return PowerSnapshot{AC: ACUnknown, State: BatteryUnknown}
	}

	p := decodeSystemPowerStatus(raw)
	if !p.Present {
		return p
	}
	augmentBatteryRate(&p)
	return p
}

// augmentBatteryRate fills in the charge/discharge rate from
// CallNtPowerInformation.  Purely additive: GetSystemPowerStatus stays
// authoritative for percentage and AC state, and any failure here just leaves
// RateValid false.
func augmentBatteryRate(p *PowerSnapshot) {
	if err := procCallNtPowerInformation.Find(); err != nil {
		return
	}
	var sbs systemBatteryStateStruct
	r, _, _ := procCallNtPowerInformation.Call(
		uintptr(systemBatteryState),
		0, 0,
		uintptr(unsafe.Pointer(&sbs)),
		unsafe.Sizeof(sbs),
	)
	if r != 0 { // STATUS_SUCCESS == 0
		return
	}
	if sbs.BatteryPresent == 0 || sbs.Rate == rateUnknown {
		return
	}
	rate := sbs.Rate
	// Some drivers report a magnitude and signal direction via the flags instead
	// of signing Rate; normalise so the sign always means direction.
	if sbs.Discharging != 0 && rate > 0 {
		rate = -rate
	}
	p.RateMilliW, p.RateValid = rate, true
}
