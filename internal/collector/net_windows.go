//go:build windows

package collector

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modIphlpapi      = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfTable2  = modIphlpapi.NewProc("GetIfTable2")
	procFreeMibTable = modIphlpapi.NewProc("FreeMibTable")
)

const ifTypeSoftwareLoopback = 24

type mibIfRow2 struct {
	InterfaceLuid               uint64
	InterfaceIndex              uint32
	InterfaceGuid               windows.GUID
	Alias                       [257]uint16
	Description                 [257]uint16
	PhysicalAddressLength       uint32
	PhysicalAddress             [32]byte
	PermanentPhysicalAddress    [32]byte
	Mtu                         uint32
	Type                        uint32
	TunnelType                  uint32
	MediaType                   uint32
	PhysicalMediumType          uint32
	AccessType                  uint32
	DirectionType               uint32
	InterfaceAndOperStatusFlags uint8
	OperStatus                  uint32
	AdminStatus                 uint32
	MediaConnectState           uint32
	NetworkGuid                 windows.GUID
	ConnectionType              uint32
	_                           [4]byte
	TransmitLinkSpeed           uint64
	ReceiveLinkSpeed            uint64
	InOctets                    uint64
	InUcastPkts                 uint64
	InNUcastPkts                uint64
	InDiscards                  uint64
	InErrors                    uint64
	InUnknownProtos             uint64
	InUcastOctets               uint64
	InMulticastOctets           uint64
	InBroadcastOctets           uint64
	OutOctets                   uint64
	OutUcastPkts                uint64
	OutNUcastPkts               uint64
	OutDiscards                 uint64
	OutErrors                   uint64
	OutUcastOctets              uint64
	OutMulticastOctets          uint64
	OutBroadcastOctets          uint64
	OutQLen                     uint64
}

type mibIfTable2 struct {
	NumEntries uint32
	_          uint32
	Table      [1]mibIfRow2
}

func collectNetNative(
	prevBytes map[string][2]uint64,
	prevTime time.Time,
) ([]NetSnapshot, map[string][2]uint64, time.Time) {
	now := time.Now()
	var pTable *mibIfTable2
	r, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&pTable)))
	if r != 0 || pTable == nil {
		return nil, prevBytes, prevTime
	}
	defer func() {
		_, _, _ = procFreeMibTable.Call(uintptr(unsafe.Pointer(pTable)))
	}()

	numEntries := int(pTable.NumEntries)
	rows := unsafe.Slice(&pTable.Table[0], numEntries)

	newBytes := make(map[string][2]uint64, numEntries)
	snaps := make([]NetSnapshot, 0, numEntries)

	elapsed := now.Sub(prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	for i := range rows {
		row := &rows[i]
		if row.Type == ifTypeSoftwareLoopback {
			continue
		}
		if row.InOctets == 0 && row.OutOctets == 0 {
			continue
		}

		var name string
		if row.Alias[0] != 0 {
			name = windows.UTF16ToString(row.Alias[:])
		} else if row.Description[0] != 0 {
			name = windows.UTF16ToString(row.Description[:])
		} else {
			continue
		}

		newBytes[name] = [2]uint64{row.OutOctets, row.InOctets}

		var sentPerSec, recvPerSec float64
		if prev, ok := prevBytes[name]; ok && !prevTime.IsZero() {
			sentPerSec = netRate(row.OutOctets, prev[0], elapsed)
			recvPerSec = netRate(row.InOctets, prev[1], elapsed)
		}

		snaps = append(snaps, NetSnapshot{
			Name:            name,
			BytesSentPerSec: sentPerSec,
			BytesRecvPerSec: recvPerSec,
		})
	}

	return snaps, newBytes, now
}
