package meshtastic

import (
	"errors"
	"fmt"
)

// Meshtastic RegionCode values (config.proto) that matter here.
const (
	regionUnset = 0
	// The 433 MHz regions: license-exempt ISM/SRD (433.05–434.79 MHz) in
	// ITU Region 1 and parts of Region 3, but inside the 70 cm amateur band
	// (420–450 MHz) in Region 2, the Americas.
	regionEU433  = 2
	regionUA433  = 14
	regionMY433  = 16
	regionPH433  = 19
	regionANZ433 = 22
	regionKZ433  = 23
	// The amateur-band regions firmware ≥2.6 offers for licensed operators.
	regionITU12M   = 27
	regionITU22M   = 28
	regionITU32M   = 33
	regionITU170CM = 34
	regionITU270CM = 35
	regionITU370CM = 36
	regionITU2125  = 37
)

var regionNames = map[uint32]string{
	1: "US", 2: "EU_433", 3: "EU_868", 4: "CN", 5: "JP", 6: "ANZ", 7: "KR", 8: "TW", 9: "RU",
	10: "IN", 11: "NZ_865", 12: "TH", 13: "LORA_24", 14: "UA_433", 15: "UA_868", 16: "MY_433",
	17: "MY_919", 18: "SG_923", 19: "PH_433", 20: "PH_868", 21: "PH_915", 22: "ANZ_433",
	23: "KZ_433", 24: "KZ_863", 25: "NP_865", 26: "BR_902", 29: "EU_866", 30: "EU_874",
	31: "EU_917", 32: "EU_N_868",
}

var is433 = map[uint32]bool{regionEU433: true, regionUA433: true, regionMY433: true, regionPH433: true, regionANZ433: true, regionKZ433: true}

var amateurRegion = map[uint32]bool{regionITU12M: true, regionITU22M: true, regionITU32M: true, regionITU170CM: true, regionITU270CM: true, regionITU370CM: true, regionITU2125: true}

// amateurOnly are allocations where, somewhere in the world, only licensed
// amateurs may transmit (MHz). An override frequency inside one is refused
// outright, except the 433 ISM sub-band when the user has said 433 is
// license-exempt where they are.
var amateurOnly = [][2]float64{{50, 54}, {144, 148}, {219, 225}, {420, 450}, {1240, 1300}}

// ErrRegion is the class of refusals on radio region grounds.
var ErrRegion = errors.New("meshtastic: radio region")

// checkRegion fails closed: only a known, license-exempt region (and, if
// set, an override frequency outside amateur-only bands) passes.
func checkRegion(lc *LoRaConfig, allow433 bool) error {
	if lc == nil {
		return fmt.Errorf("%w: the node sent no LoRa config, so its band cannot be checked", ErrRegion)
	}
	r := lc.Region
	switch {
	case r == regionUnset:
		return fmt.Errorf("%w is unset; set it to your country's band in the Meshtastic app", ErrRegion)
	case amateurRegion[r]:
		return fmt.Errorf("%w %d is an amateur band, where encrypted traffic is unlawful", ErrRegion, r)
	case is433[r] && !allow433:
		return fmt.Errorf("%w %s is 433 MHz, which is amateur-only in the Americas; if 433 MHz is license-exempt where you are, set \"allow433\": true in meshtastic.json", ErrRegion, regionNames[r])
	case regionNames[r] == "":
		return fmt.Errorf("%w %d is unknown to Concord; refusing rather than guess", ErrRegion, r)
	}
	if f := float64(lc.OverrideFrequency); f != 0 {
		for _, b := range amateurOnly {
			if f >= b[0] && f <= b[1] {
				if allow433 && is433[r] && f >= 433.05 && f <= 434.79 {
					continue
				}
				return fmt.Errorf("%w: override frequency %.3f MHz is in an amateur band (%g–%g MHz)", ErrRegion, f, b[0], b[1])
			}
		}
	}
	return nil
}
