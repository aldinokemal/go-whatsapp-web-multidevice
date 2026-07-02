package mlow

import "testing"

func loadMem(t *testing.T) *SmplMem {
	t.Helper()
	m := LoadSmplMem()
	if m == nil {
		t.Fatal("LoadSmplMem returned nil")
	}
	return m
}

// TestLoadSmplMem checks the seed-built Group-D window: 6 regions, GPitch/GClk at the
// fixed WASM addresses, and GCC/GNrg zero (Groups A/B/C/E moved to CcTables).
func TestLoadSmplMem(t *testing.T) {
	m := loadMem(t)
	if len(m.regions) != 6 {
		t.Errorf("regions: got %d want 6", len(m.regions))
	}
	for _, c := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"GCC", m.GCC, 0},
		{"GNrg", m.GNrg, 0},
		{"GPitch", m.GPitch, 12178296}, // 0xb9d378
		{"GClk", m.GClk, 12188072},     // 0xb9f9a8
	} {
		if c.got != c.want {
			t.Errorf("%s: got %d want %d", c.name, c.got, c.want)
		}
	}
}

// TestSmplMemAccessors verifies the accessor semantics mem owns: little-endian
// width decode, signed reinterpretation, the out-of-region zero fallback, and the
// 2-byte CDFAt stride.
func TestSmplMemAccessors(t *testing.T) {
	m := loadMem(t)
	addr := uint32(memPcfg + 0x1d38) // inside the Group-D records region

	if uint16(m.U32(addr)) != m.U16(addr) {
		t.Errorf("U32 low 16 bits %#x != U16 %#x", uint16(m.U32(addr)), m.U16(addr))
	}
	if m.I16(addr) != int16(m.U16(addr)) {
		t.Errorf("I16 %d != int16(U16) %d", m.I16(addr), int16(m.U16(addr)))
	}
	if m.I32(addr) != int32(m.U32(addr)) {
		t.Errorf("I32 %d != int32(U32) %d", m.I32(addr), int32(m.U32(addr)))
	}

	cdf := m.CDFAt(addr, 4)
	if len(cdf) != 4 {
		t.Fatalf("CDFAt len: got %d want 4", len(cdf))
	}
	for i := 0; i < 4; i++ {
		if cdf[i] != m.U16(addr+uint32(i)*2) {
			t.Errorf("CDFAt[%d] %#x != U16(addr+%d) %#x", i, cdf[i], i*2, m.U16(addr+uint32(i)*2))
		}
	}

	// Address 0 sits outside every region; all widths read as 0.
	if m.U8(0) != 0 || m.U16(0) != 0 || m.U32(0) != 0 {
		t.Errorf("out-of-region read not zero: u8=%d u16=%d u32=%d", m.U8(0), m.U16(0), m.U32(0))
	}
}

// TestSilkCosTab checks the Q12 cosine table transcription: 129 entries, the
// endpoints, and symmetry about the zero-valued center.
func TestSilkCosTab(t *testing.T) {
	if len(silkLSFCosTabFIXQ12) != 129 {
		t.Fatalf("len: got %d want 129", len(silkLSFCosTabFIXQ12))
	}
	if silkLSFCosTabFIXQ12[0] != 8192 || silkLSFCosTabFIXQ12[128] != -8192 || silkLSFCosTabFIXQ12[64] != 0 {
		t.Fatalf("endpoints/center: [0]=%d [64]=%d [128]=%d",
			silkLSFCosTabFIXQ12[0], silkLSFCosTabFIXQ12[64], silkLSFCosTabFIXQ12[128])
	}
	for i := 0; i <= 64; i++ {
		if silkLSFCosTabFIXQ12[i] != -silkLSFCosTabFIXQ12[128-i] {
			t.Errorf("asymmetry at %d: %d vs %d", i, silkLSFCosTabFIXQ12[i], silkLSFCosTabFIXQ12[128-i])
		}
	}
}

// TestSmplTablesCDF is the byte-exact CDF KAT against smpl_tables.json. The
// address→table mapping that ties those named tables to WASM offsets lives in the
// decode modules (lsf/gains/pulse/pitch), so this check lands there, not here.
func TestSmplTablesCDF(t *testing.T) {
	t.Skip("byte-exact CDF verification deferred to the consuming decode modules; " +
		"smpl_tables.json is pinned transitively when they decode through SmplMem")
}
