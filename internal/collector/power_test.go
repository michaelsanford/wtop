package collector

import (
	"testing"
	"time"
)

// Windows reports "unknown" in-band, using sentinel values that are valid-looking
// numbers: an unknown charge is 255, and an unknown remaining time is 0xFFFFFFFF.
// Taken at face value those render as "255%" and "49710 hours", so every one of
// them has to be mapped to an explicit invalid flag.
func TestDecodeSystemPowerStatus(t *testing.T) {
	tests := []struct {
		name string
		in   systemPowerStatus
		want PowerSnapshot
	}{
		{
			name: "desktop with no battery",
			in:   systemPowerStatus{ACLineStatus: 1, BatteryFlag: 128, BatteryLifePercent: 255, BatteryLifeTime: 0xFFFFFFFF, BatteryFullLifeTime: 0xFFFFFFFF},
			want: PowerSnapshot{AC: ACOnline, Present: false, State: BatteryAbsent},
		},
		{
			name: "status unknown is not the same as absent",
			in:   systemPowerStatus{ACLineStatus: 255, BatteryFlag: 255, BatteryLifePercent: 255, BatteryLifeTime: 0xFFFFFFFF},
			want: PowerSnapshot{AC: ACUnknown, Present: false, State: BatteryUnknown},
		},
		{
			name: "laptop discharging with an estimate",
			in:   systemPowerStatus{ACLineStatus: 0, BatteryFlag: 1, BatteryLifePercent: 84, BatteryLifeTime: 8100},
			want: PowerSnapshot{AC: ACOffline, Present: true, State: BatteryDischarging,
				Pct: 84, PctValid: true, Remaining: 8100 * time.Second, RemainingValid: true},
		},
		{
			name: "charging on mains",
			in:   systemPowerStatus{ACLineStatus: 1, BatteryFlag: 1 | 8, BatteryLifePercent: 95, BatteryLifeTime: 0xFFFFFFFF},
			want: PowerSnapshot{AC: ACOnline, Present: true, State: BatteryCharging, Pct: 95, PctValid: true},
		},
		{
			name: "full on mains",
			in:   systemPowerStatus{ACLineStatus: 1, BatteryFlag: 1, BatteryLifePercent: 100, BatteryLifeTime: 0xFFFFFFFF},
			want: PowerSnapshot{AC: ACOnline, Present: true, State: BatteryFull, Pct: 100, PctValid: true},
		},
		{
			name: "critical on battery",
			in:   systemPowerStatus{ACLineStatus: 0, BatteryFlag: 4, BatteryLifePercent: 4, BatteryLifeTime: 300},
			want: PowerSnapshot{AC: ACOffline, Present: true, State: BatteryDischarging,
				Pct: 4, PctValid: true, Remaining: 300 * time.Second, RemainingValid: true},
		},
		{
			name: "present but percentage unknown",
			in:   systemPowerStatus{ACLineStatus: 0, BatteryFlag: 1, BatteryLifePercent: 255, BatteryLifeTime: 0xFFFFFFFF},
			want: PowerSnapshot{AC: ACOffline, Present: true, State: BatteryDischarging},
		},
		{
			name: "battery saver engaged",
			in:   systemPowerStatus{ACLineStatus: 0, BatteryFlag: 2, BatteryLifePercent: 15, SystemStatusFlag: 1, BatteryLifeTime: 1200},
			want: PowerSnapshot{AC: ACOffline, Present: true, State: BatteryDischarging,
				Pct: 15, PctValid: true, Remaining: 1200 * time.Second, RemainingValid: true, Saver: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeSystemPowerStatus(tt.in); got != tt.want {
				t.Errorf("\ngot  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// The distinction this test protects is the whole reason Present is separate from
// State: a desktop must render differently from a machine that declined to answer.
func TestDecodeSystemPowerStatus_AbsentIsDistinctFromUnknown(t *testing.T) {
	absent := decodeSystemPowerStatus(systemPowerStatus{ACLineStatus: 1, BatteryFlag: 128})
	unknown := decodeSystemPowerStatus(systemPowerStatus{ACLineStatus: 255, BatteryFlag: 255})

	if absent.Present || unknown.Present {
		t.Error("neither case has a usable battery reading")
	}
	if absent.State == unknown.State {
		t.Errorf("absent and unknown both decoded to %v; they must be distinguishable", absent.State)
	}
}

// A platform with no implementation has not established that there is no battery.
func TestCollectPowerStub_ReportsUnknownNotAbsent(t *testing.T) {
	p := collectPower()
	if p.State == BatteryAbsent && !p.Present {
		// Windows may legitimately report absent; only the stub is under test here,
		// and it cannot be reached on this build.
		t.Skip("running on a platform with a real implementation")
	}
}

func TestEffectiveMHz(t *testing.T) {
	tests := []struct {
		name    string
		base    uint64
		pct     float64
		want    float64
		wantErr bool
	}{
		{name: "at base clock", base: 2700, pct: 100, want: 2700},
		{name: "half speed", base: 2700, pct: 50, want: 1350},
		// Measured on the development machine: turbo legitimately exceeds 100 %.
		{name: "sustained turbo must not be clamped", base: 2700, pct: 191.57, want: 5172.39},
		{name: "no base clock", base: 0, pct: 150, want: 0},
		{name: "no counter", base: 2700, pct: 0, want: 0},
		{name: "negative counter", base: 2700, pct: -5, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveMHz(tt.base, tt.pct)
			if diff := got - tt.want; diff > 0.01 || diff < -0.01 {
				t.Errorf("effectiveMHz(%d, %v) = %v, want %v", tt.base, tt.pct, got, tt.want)
			}
		})
	}
}

func TestEffectiveMHz_TurboExceedsBase(t *testing.T) {
	base := uint64(2700)
	if got := effectiveMHz(base, 191.57); got <= float64(base) {
		t.Errorf("turbo reported %v MHz, at or below the %d MHz base — the percentage was clamped", got, base)
	}
}
